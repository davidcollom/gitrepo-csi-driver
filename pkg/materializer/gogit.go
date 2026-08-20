package materializer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/davidcollom/gitrepo-csi-driver/pkg/policy"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/volume"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	goconfig "github.com/go-git/go-git/v5/plumbing/format/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	gogittransport "github.com/go-git/go-git/v5/plumbing/transport"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

type goGitBackend struct {
	uid *uint32
	gid *uint32
}

// NewGoGit returns a Backend that clones entirely in-memory and writes the
// content tree to disk, applying UID/GID ownership via os.Lchown rather than
// running a subprocess with SysProcAttr.Credential.
func NewGoGit() (Backend, error) {
	b := &goGitBackend{}
	uidRaw := os.Getenv("GITCONTENT_GIT_RUN_AS_UID")
	gidRaw := os.Getenv("GITCONTENT_GIT_RUN_AS_GID")
	if (uidRaw == "") != (gidRaw == "") {
		return nil, fmt.Errorf("both GITCONTENT_GIT_RUN_AS_UID and GITCONTENT_GIT_RUN_AS_GID are required")
	}
	if uidRaw != "" {
		uid, err := strconv.ParseUint(uidRaw, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid GITCONTENT_GIT_RUN_AS_UID: %w", err)
		}
		gid, err := strconv.ParseUint(gidRaw, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid GITCONTENT_GIT_RUN_AS_GID: %w", err)
		}
		u, g := uint32(uid), uint32(gid)
		b.uid, b.gid = &u, &g
	}
	return b, nil
}

func (b *goGitBackend) Materialize(ctx context.Context, attrs volume.Attributes, p policy.GitContentPolicy, workDir string) (Result, error) {
	if err := os.MkdirAll(workDir, privateDirPerm); err != nil {
		return Result{}, err
	}

	depth := attrs.Depth
	if depth == 0 {
		depth = p.Clone.DefaultDepth
	}

	cloneOpts := &gogit.CloneOptions{
		URL:        attrs.Repo,
		Depth:      depth,
		NoCheckout: true,
	}
	if ref := mutableRef(attrs); ref != "" && !strings.EqualFold(ref, "HEAD") {
		if strings.Contains(attrs.RevisionKind, "tag") ||
			strings.HasPrefix(attrs.Revision, "refs/tags/") ||
			strings.HasPrefix(attrs.Revision, "tag:") {
			cloneOpts.ReferenceName = plumbing.NewTagReferenceName(ref)
		} else {
			cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(ref)
		}
		cloneOpts.SingleBranch = true
	}
	if user := os.Getenv("GITCONTENT_HTTP_USERNAME"); user != "" {
		cloneOpts.Auth = &gogithttp.BasicAuth{
			Username: user,
			Password: os.Getenv("GITCONTENT_HTTP_PASSWORD"),
		}
	}

	repo, err := gogit.CloneContext(ctx, memory.NewStorage(), nil, cloneOpts)
	if err != nil {
		return Result{}, fmt.Errorf("git clone failed: %w", err)
	}

	hash, err := b.resolveHash(repo, attrs)
	if err != nil {
		return Result{}, err
	}

	commit, err := repo.CommitObject(hash)
	if err != nil {
		return Result{}, fmt.Errorf("resolve commit failed: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return Result{}, fmt.Errorf("get tree failed: %w", err)
	}

	if attrs.Path != "" {
		cleaned := filepath.ToSlash(filepath.Clean(attrs.Path))
		if !filepath.IsLocal(filepath.FromSlash(cleaned)) || hasParentPathSegment(cleaned) || hasGitPathSegment(cleaned) {
			return Result{}, fmt.Errorf("invalid path %s", attrs.Path)
		}
		tree, err = tree.Tree(cleaned)
		if err != nil {
			return Result{}, fmt.Errorf("path %s not found in repository: %w", attrs.Path, err)
		}
	}

	contentDir := filepath.Join(workDir, "content")
	if err := os.RemoveAll(contentDir); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(contentDir, publicDirPerm); err != nil { // #nosec G301 -- mounted content must be traversable by workload users.
		return Result{}, err
	}

	files, size, err := b.exportTree(tree, contentDir)
	if err != nil {
		return Result{}, err
	}

	if attrs.Submodules {
		sf, ss, serr := b.exportSubmodules(ctx, tree, contentDir, cloneOpts.Auth, p.Submodules.MaxDepth)
		if serr != nil {
			return Result{}, serr
		}
		files += sf
		size += ss
	}

	if p.Clone.MaxFileCount > 0 && files > p.Clone.MaxFileCount {
		return Result{}, fmt.Errorf("repository file count %d exceeds limit %d", files, p.Clone.MaxFileCount)
	}
	if p.Clone.MaxRepositorySize > 0 && size > p.Clone.MaxRepositorySize {
		return Result{}, fmt.Errorf("repository size %d exceeds limit %d", size, p.Clone.MaxRepositorySize)
	}

	if b.uid != nil {
		if err := chownTree(contentDir, int(*b.uid), int(*b.gid)); err != nil {
			return Result{}, err
		}
	}

	return Result{
		ResolvedRevision: commit.Hash.String(),
		MountedPath:      contentDir,
		FileCount:        files,
		SizeBytes:        size,
	}, nil
}

func (b *goGitBackend) Refresh(ctx context.Context, attrs volume.Attributes, p policy.GitContentPolicy, workDir string) (Result, error) {
	// Re-clone from memory; for mutable refs this always fetches the latest state.
	return b.Materialize(ctx, attrs, p, workDir)
}

func (b *goGitBackend) resolveHash(repo *gogit.Repository, attrs volume.Attributes) (plumbing.Hash, error) {
	if isPinnedCommit(attrs.Revision) {
		return plumbing.NewHash(attrs.Revision), nil
	}
	ref, err := repo.Head()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("resolve HEAD failed: %w", err)
	}
	return ref.Hash(), nil
}

func (b *goGitBackend) exportTree(tree *object.Tree, contentDir string) (int64, int64, error) {
	var fileCount, totalSize int64
	err := tree.Files().ForEach(func(f *object.File) error {
		if hasGitPathSegment(f.Name) {
			return nil
		}
		target, err := safeJoin(contentDir, filepath.FromSlash(f.Name))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), publicDirPerm); err != nil { // #nosec G301 -- parent directories for mounted content must be traversable.
			return err
		}

		if f.Mode == filemode.Symlink {
			return b.exportSymlink(f, target, contentDir)
		}
		if !f.Mode.IsFile() {
			return nil
		}

		n, err := b.exportRegularFile(f, target)
		if err != nil {
			return err
		}
		fileCount++
		totalSize += n
		return nil
	})
	return fileCount, totalSize, err
}

func (b *goGitBackend) exportSymlink(f *object.File, target, contentDir string) error {
	linkTarget, err := f.Contents()
	if err != nil {
		return err
	}
	if err := validateSymlinkTarget(contentDir, target, linkTarget); err != nil {
		return err
	}
	return os.Symlink(linkTarget, target)
}

func (b *goGitBackend) exportRegularFile(f *object.File, target string) (int64, error) {
	reader, err := f.Reader()
	if err != nil {
		return 0, err
	}
	defer func() { _ = reader.Close() }()

	perm := os.FileMode(f.Mode).Perm() & 0o555
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm) // #nosec G304 -- target is rooted by safeJoin.
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(out, reader)
	if closeErr := out.Close(); closeErr != nil && copyErr == nil {
		copyErr = closeErr
	}
	return n, copyErr
}

// chownTree sets uid/gid ownership on all entries under root, including directories.
func chownTree(root string, uid, gid int) error {
	return filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(path, uid, gid)
	})
}

// exportSubmodules clones each submodule declared in .gitmodules and exports its tree.
func (b *goGitBackend) exportSubmodules(ctx context.Context, tree *object.Tree, contentDir string, auth gogittransport.AuthMethod, maxDepth int) (int64, int64, error) {
	moduleURLs, err := parseGitModules(tree)
	if err != nil || len(moduleURLs) == 0 {
		return 0, 0, err
	}

	walker := object.NewTreeWalker(tree, true, make(map[plumbing.Hash]bool))
	defer walker.Close()

	var totalFiles, totalSize int64
	for {
		name, entry, err := walker.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, 0, err
		}
		if entry.Mode != filemode.Submodule {
			continue
		}
		url, ok := moduleURLs[name]
		if !ok {
			continue
		}
		subDir, err := safeJoin(contentDir, filepath.FromSlash(name))
		if err != nil {
			return 0, 0, err
		}
		if err := os.MkdirAll(subDir, publicDirPerm); err != nil { // #nosec G301
			return 0, 0, err
		}
		f, s, err := b.cloneAndExport(ctx, url, entry.Hash, subDir, auth, maxDepth)
		if err != nil {
			return 0, 0, fmt.Errorf("submodule %s: %w", name, err)
		}
		totalFiles += f
		totalSize += s
	}
	return totalFiles, totalSize, nil
}

func (b *goGitBackend) cloneAndExport(ctx context.Context, url string, commitHash plumbing.Hash, destDir string, auth gogittransport.AuthMethod, depth int) (int64, int64, error) {
	opts := &gogit.CloneOptions{
		URL:        url,
		Depth:      depth,
		NoCheckout: true,
		Auth:       auth,
	}
	repo, err := gogit.CloneContext(ctx, memory.NewStorage(), nil, opts)
	if err != nil {
		return 0, 0, fmt.Errorf("clone failed: %w", err)
	}
	commit, err := repo.CommitObject(commitHash)
	if err != nil {
		// Fall back to HEAD if the pinned hash isn't in the shallow clone.
		ref, herr := repo.Head()
		if herr != nil {
			return 0, 0, fmt.Errorf("resolve commit: %w", err)
		}
		commit, err = repo.CommitObject(ref.Hash())
		if err != nil {
			return 0, 0, fmt.Errorf("resolve commit: %w", err)
		}
	}
	tree, err := commit.Tree()
	if err != nil {
		return 0, 0, err
	}
	return b.exportTree(tree, destDir)
}

// parseGitModules reads .gitmodules and returns a map of submodule path → URL.
func parseGitModules(tree *object.Tree) (map[string]string, error) {
	f, err := tree.File(".gitmodules")
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return nil, nil
		}
		return nil, err
	}
	contents, err := f.Contents()
	if err != nil {
		return nil, err
	}
	cfg := goconfig.New()
	if err := goconfig.NewDecoder(strings.NewReader(contents)).Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse .gitmodules: %w", err)
	}
	modules := make(map[string]string)
	for _, sec := range cfg.Sections {
		if !strings.EqualFold(sec.Name, "submodule") {
			continue
		}
		for _, sub := range sec.Subsections {
			path := sub.Options.Get("path")
			url := sub.Options.Get("url")
			if path != "" && url != "" {
				modules[filepath.ToSlash(path)] = url
			}
		}
	}
	return modules, nil
}
