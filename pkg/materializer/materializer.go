package materializer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/davidcollom/gitrepo-csi-driver/pkg/policy"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/volume"
)

type Backend interface {
	Materialize(ctx context.Context, attrs volume.Attributes, p policy.GitContentPolicy, workDir string) (Result, error)
	Refresh(ctx context.Context, attrs volume.Attributes, p policy.GitContentPolicy, workDir string) (Result, error)
}

type Result struct {
	ResolvedRevision string
	RepoPath         string
	MountedPath      string
	FileCount        int64
	SizeBytes        int64
	CacheHit         bool
}

type gitBinaryBackend struct{}

func New() Backend {
	return &gitBinaryBackend{}
}

func (m *gitBinaryBackend) Materialize(ctx context.Context, attrs volume.Attributes, p policy.GitContentPolicy, workDir string) (Result, error) {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return Result{}, err
	}
	if err := prepareWorkDirForGit(workDir); err != nil {
		return Result{}, err
	}

	repoDir := filepath.Join(workDir, "repo")
	contentDir := filepath.Join(workDir, "content")

	depth := attrs.Depth
	if depth == 0 {
		depth = p.Clone.DefaultDepth
	}

	if err := m.cloneOrRefresh(ctx, attrs, p, workDir, repoDir, depth); err != nil {
		return Result{}, err
	}

	if err := m.checkoutRevision(ctx, attrs, repoDir); err != nil {
		return Result{}, err
	}
	if attrs.Submodules {
		if err := runGit(ctx, repoDir, "submodule", "update", "--init", "--depth", strconv.Itoa(p.Submodules.MaxDepth)); err != nil {
			return Result{}, fmt.Errorf("git submodule update failed: %w", err)
		}
	}

	resolved, err := gitOutput(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, fmt.Errorf("resolve revision failed: %w", err)
	}

	sourcePath := repoDir
	if attrs.Path != "" {
		sourcePath = filepath.Join(repoDir, filepath.Clean(attrs.Path))
	}
	if err := ensureInside(repoDir, sourcePath); err != nil {
		return Result{}, err
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("requested path %s is not a directory", attrs.Path)
	}

	if err := os.RemoveAll(contentDir); err != nil {
		return Result{}, err
	}
	if err := CopyContentTree(sourcePath, contentDir); err != nil {
		return Result{}, err
	}

	files, size, err := countFiles(contentDir)
	if err != nil {
		return Result{}, err
	}

	if p.Clone.MaxFileCount > 0 && files > p.Clone.MaxFileCount {
		return Result{}, fmt.Errorf("repository file count %d exceeds limit %d", files, p.Clone.MaxFileCount)
	}
	if p.Clone.MaxRepositorySize > 0 && size > p.Clone.MaxRepositorySize {
		return Result{}, fmt.Errorf("repository size %d exceeds limit %d", size, p.Clone.MaxRepositorySize)
	}

	return Result{
		ResolvedRevision: resolved,
		RepoPath:         repoDir,
		MountedPath:      contentDir,
		FileCount:        files,
		SizeBytes:        size,
	}, nil
}

func (m *gitBinaryBackend) Refresh(ctx context.Context, attrs volume.Attributes, p policy.GitContentPolicy, workDir string) (Result, error) {
	return m.Materialize(ctx, attrs, p, workDir)
}

func (m *gitBinaryBackend) cloneOrRefresh(ctx context.Context, attrs volume.Attributes, p policy.GitContentPolicy, workDir, repoDir string, depth int) error {
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err == nil {
		if isMutableRevision(attrs) {
			return m.fetchMutableRevision(ctx, attrs, repoDir)
		}
		return nil
	}

	if isPinnedCommit(attrs.Revision) {
		if err := runGit(ctx, workDir, "clone", "--no-checkout", "--depth", strconv.Itoa(depth), attrs.Repo, repoDir); err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}
		return nil
	}

	branchOrTag := mutableRef(attrs)
	args := []string{"clone", "--depth", strconv.Itoa(depth)}
	if branchOrTag != "" {
		args = append(args, "--branch", branchOrTag)
	}
	args = append(args, attrs.Repo, repoDir)
	if err := runGit(ctx, workDir, args...); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return nil
}

func (m *gitBinaryBackend) fetchMutableRevision(ctx context.Context, attrs volume.Attributes, repoDir string) error {
	branchOrTag := mutableRef(attrs)
	if branchOrTag == "" {
		return nil
	}
	fetchArgs := []string{"fetch", "--prune", "--depth", strconv.Itoa(max(1, attrs.Depth))}
	if strings.Contains(attrs.RevisionKind, "tag") {
		fetchArgs = append(fetchArgs, "--tags")
	}
	fetchArgs = append(fetchArgs, "origin", branchOrTag)
	if err := runGit(ctx, repoDir, fetchArgs...); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}
	return nil
}

func (m *gitBinaryBackend) checkoutRevision(ctx context.Context, attrs volume.Attributes, repoDir string) error {
	if isPinnedCommit(attrs.Revision) {
		if err := runGit(ctx, repoDir, "checkout", "--detach", attrs.Revision); err != nil {
			return fmt.Errorf("git checkout failed: %w", err)
		}
		return nil
	}
	ref := mutableRef(attrs)
	if ref == "" {
		ref = attrs.Revision
	}
	if err := runGit(ctx, repoDir, "checkout", "--force", ref); err != nil {
		return fmt.Errorf("git checkout failed: %w", err)
	}
	return nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd, err := gitCommand(ctx, dir, args...)
	if err != nil {
		return err
	}
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0", "GIT_LFS_SKIP_SMUDGE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd, err := gitCommand(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return stringTrim(string(out)), nil
}

func gitCommand(ctx context.Context, dir string, args ...string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "git", gitArgs(args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
	)
	if cred, ok, err := gitCredential(); err != nil {
		return nil, err
	} else if ok {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
	}
	return cmd, nil
}

func gitCredential() (*syscall.Credential, bool, error) {
	uidRaw := os.Getenv("GITCONTENT_GIT_RUN_AS_UID")
	gidRaw := os.Getenv("GITCONTENT_GIT_RUN_AS_GID")
	if uidRaw == "" && gidRaw == "" {
		return nil, false, nil
	}
	if os.Geteuid() != 0 {
		return nil, false, nil
	}
	if uidRaw == "" || gidRaw == "" {
		return nil, false, fmt.Errorf("both GITCONTENT_GIT_RUN_AS_UID and GITCONTENT_GIT_RUN_AS_GID are required")
	}
	uid, err := strconv.ParseUint(uidRaw, 10, 32)
	if err != nil {
		return nil, false, fmt.Errorf("invalid GITCONTENT_GIT_RUN_AS_UID: %w", err)
	}
	gid, err := strconv.ParseUint(gidRaw, 10, 32)
	if err != nil {
		return nil, false, fmt.Errorf("invalid GITCONTENT_GIT_RUN_AS_GID: %w", err)
	}
	return &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}, true, nil
}

func prepareWorkDirForGit(path string) error {
	_, ok, err := gitCredential()
	if err != nil || !ok {
		return err
	}
	if err := os.Chmod(path, 0o775); err != nil && !errors.Is(err, os.ErrPermission) {
		return err
	}
	return nil
}

func gitArgs(args ...string) []string {
	hardened := []string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "protocol.file.allow=never",
	}
	return append(hardened, args...)
}

func ensureInside(root, candidate string) error {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("requested path escapes repository")
	}
	return nil
}

func CopyContentTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if hasGitPathSegment(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if mode.Type() == os.ModeSymlink {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		}
		if d.IsDir() {
			return os.MkdirAll(target, mode.Perm())
		}
		if !mode.IsRegular() {
			return nil
		}
		return copyRegularFile(path, target, mode.Perm())
	})
}

func copyRegularFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func hasGitPathSegment(rel string) bool {
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == ".git" {
			return true
		}
	}
	return false
}

func stringTrim(v string) string {
	for len(v) > 0 && (v[len(v)-1] == '\n' || v[len(v)-1] == '\r' || v[len(v)-1] == ' ') {
		v = v[:len(v)-1]
	}
	return v
}

func isPinnedCommit(revision string) bool {
	return len(revision) == 40 && isHex(revision)
}

func isMutableRevision(attrs volume.Attributes) bool {
	return !isPinnedCommit(attrs.Revision)
}

func mutableRef(attrs volume.Attributes) string {
	if attrs.RevisionKind == "tag" {
		return strings.TrimPrefix(strings.TrimPrefix(attrs.Revision, "refs/tags/"), "tag:")
	}
	if attrs.RevisionKind == "branch" {
		return strings.TrimPrefix(strings.TrimPrefix(attrs.Revision, "refs/heads/"), "branch:")
	}
	if strings.HasPrefix(attrs.Revision, "refs/tags/") {
		return strings.TrimPrefix(attrs.Revision, "refs/tags/")
	}
	if strings.HasPrefix(attrs.Revision, "refs/heads/") {
		return strings.TrimPrefix(attrs.Revision, "refs/heads/")
	}
	if strings.HasPrefix(attrs.Revision, "tag:") {
		return strings.TrimPrefix(attrs.Revision, "tag:")
	}
	if strings.HasPrefix(attrs.Revision, "branch:") {
		return strings.TrimPrefix(attrs.Revision, "branch:")
	}
	if isPinnedCommit(attrs.Revision) {
		return ""
	}
	return attrs.Revision
}

func isHex(v string) bool {
	for _, ch := range v {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
