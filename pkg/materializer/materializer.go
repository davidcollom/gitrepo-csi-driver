package materializer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

const (
	privateDirPerm os.FileMode = 0o750
	publicDirPerm  os.FileMode = 0o755
)

func ensureInside(root, candidate string) error {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("requested path escapes repository")
	}
	return nil
}

func safeJoin(root, rel string) (string, error) {
	if rel == "" {
		return filepath.Clean(root), nil
	}
	cleaned := filepath.Clean(rel)
	if !filepath.IsLocal(cleaned) || hasParentPathSegment(cleaned) {
		return "", fmt.Errorf("path escapes root")
	}
	if hasGitPathSegment(cleaned) {
		return "", fmt.Errorf("path must not reference .git")
	}
	target := filepath.Join(root, cleaned)
	if err := ensureInside(root, target); err != nil {
		return "", err
	}
	return target, nil
}

func validateSymlinkTarget(root, linkPath, linkTarget string) error {
	if filepath.IsAbs(linkTarget) {
		return fmt.Errorf("repository symlink %s uses an absolute target", linkPath)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(linkPath), linkTarget))
	if err := ensureInside(root, resolved); err != nil {
		return fmt.Errorf("repository symlink %s escapes source root", linkPath)
	}
	return nil
}

func hasParentPathSegment(rel string) bool {
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == ".." {
			return true
		}
	}
	return false
}

func hasGitPathSegment(rel string) bool {
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == ".git" {
			return true
		}
	}
	return false
}

func isPinnedCommit(revision string) bool {
	return len(revision) == 40 && isHex(revision)
}

func isHex(v string) bool {
	for _, ch := range v {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
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
