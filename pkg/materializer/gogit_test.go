package materializer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davidcollom/gitrepo-csi-driver/pkg/policy"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/volume"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// newTestRepo creates a git repo in a temp directory and returns its path and
// the HEAD commit SHA.
func newTestRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()

	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	w, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	for path, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll for %s: %v", path, err)
		}
		if target, ok := strings.CutPrefix(content, "symlink:"); ok {
			if err := os.Symlink(target, full); err != nil {
				t.Fatalf("Symlink %s→%s: %v", path, target, err)
			}
		} else {
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				t.Fatalf("WriteFile %s: %v", path, err)
			}
		}
		if _, err := w.Add(path); err != nil {
			t.Fatalf("Add %s: %v", path, err)
		}
	}

	hash, err := w.Commit("test", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@t.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return dir, hash.String()
}

func newGoGit(t *testing.T) Backend {
	t.Helper()
	b, err := NewGoGit()
	if err != nil {
		t.Fatalf("NewGoGit: %v", err)
	}
	return b
}

func defaultPolicy() policy.GitContentPolicy {
	return policy.GitContentPolicy{
		Clone: policy.ClonePolicy{DefaultDepth: 1},
	}
}

func attrs(repoDir, revision string) volume.Attributes {
	return volume.Attributes{Repo: repoDir, Revision: revision}
}

// TestGoGit_BasicMaterialize verifies files are written to the content directory.
func TestGoGit_BasicMaterialize(t *testing.T) {
	repoDir, sha := newTestRepo(t, map[string]string{
		"hello.txt":        "hello",
		"sub/world.txt":    "world",
		"sub/deep/yes.txt": "yes",
	})
	b := newGoGit(t)
	workDir := t.TempDir()

	res, err := b.Materialize(t.Context(), attrs(repoDir, sha), defaultPolicy(), workDir)
	require.NoError(t, err, "Materialize")
	require.Equal(t, int64(3), res.FileCount)

	assertFileContent(t, filepath.Join(res.MountedPath, "hello.txt"), "hello")
	assertFileContent(t, filepath.Join(res.MountedPath, "sub", "world.txt"), "world")
}

// TestGoGit_PathExtraction verifies only the requested subpath is exported.
func TestGoGit_PathExtraction(t *testing.T) {
	repoDir, sha := newTestRepo(t, map[string]string{
		"a/file.txt": "a-content",
		"b/file.txt": "b-content",
	})
	b := newGoGit(t)
	workDir := t.TempDir()

	a := attrs(repoDir, sha)
	a.Path = "a"
	res, err := b.Materialize(t.Context(), a, defaultPolicy(), workDir)
	require.NoError(t, err, "Materialize")
	assertFileContent(t, filepath.Join(res.MountedPath, "file.txt"), "a-content")

	if _, err := os.Stat(filepath.Join(res.MountedPath, "b")); !os.IsNotExist(err) {
		t.Error("sibling directory b should not be present")
	}
}

// TestGoGit_PathTraversalRejected verifies attrs.Path cannot escape the repo root.
func TestGoGit_PathTraversalRejected(t *testing.T) {
	repoDir, sha := newTestRepo(t, map[string]string{"file.txt": "x"})
	b := newGoGit(t)

	for _, bad := range []string{"../sibling", "../../etc", ".git/config"} {
		a := attrs(repoDir, sha)
		a.Path = bad
		_, err := b.Materialize(t.Context(), a, defaultPolicy(), t.TempDir())
		assert.Error(t, err, "Materialize with path %q should fail", bad)
	}
}

// TestGoGit_SymlinkWithinRepo verifies that in-repo symlinks are preserved.
func TestGoGit_SymlinkWithinRepo(t *testing.T) {
	repoDir, sha := newTestRepo(t, map[string]string{
		"target.txt": "content",
		"link.txt":   "symlink:target.txt",
	})
	b := newGoGit(t)
	workDir := t.TempDir()

	res, err := b.Materialize(t.Context(), attrs(repoDir, sha), defaultPolicy(), workDir)
	require.NoError(t, err, "Materialize")

	linkDest, err := os.Readlink(filepath.Join(res.MountedPath, "link.txt"))
	require.NoError(t, err, "Readlink")
	assert.Equal(t, "target.txt", linkDest, "symlink target mismatch")
}

// TestGoGit_AbsoluteSymlinkRejected verifies that absolute symlinks are blocked.
func TestGoGit_AbsoluteSymlinkRejected(t *testing.T) {
	repoDir, sha := newTestRepo(t, map[string]string{
		"evil.txt": "symlink:/etc/passwd",
	})
	b := newGoGit(t)
	_, err := b.Materialize(t.Context(), attrs(repoDir, sha), defaultPolicy(), t.TempDir())
	require.Error(t, err, "Materialize with absolute symlink should fail")
}

// TestGoGit_EscapingSymlinkRejected verifies that symlinks escaping the content
// root are blocked.
func TestGoGit_EscapingSymlinkRejected(t *testing.T) {
	repoDir, sha := newTestRepo(t, map[string]string{
		"evil.txt": "symlink:../../etc/passwd",
	})
	b := newGoGit(t)
	_, err := b.Materialize(t.Context(), attrs(repoDir, sha), defaultPolicy(), t.TempDir())
	if err == nil {
		t.Fatal("expected error for escaping symlink, got nil")
	}
}

// TestGoGit_GitDirSkipped verifies that .git path segments in tree entries are
// skipped (defence-in-depth; go-git should never return such paths but we
// validate regardless).
func TestGoGit_GitDirSkipped(t *testing.T) {
	repoDir, sha := newTestRepo(t, map[string]string{"file.txt": "x"})
	b := newGoGit(t)
	workDir := t.TempDir()

	res, err := b.Materialize(t.Context(), attrs(repoDir, sha), defaultPolicy(), workDir)
	require.NoError(t, err, "Materialize")

	_, err = os.Stat(filepath.Join(res.MountedPath, ".git"))
	assert.True(t, os.IsNotExist(err), ".git directory must not be exported")
}

// TestGoGit_MaxFileCountEnforced verifies the file-count limit is applied.
func TestGoGit_MaxFileCountEnforced(t *testing.T) {
	repoDir, sha := newTestRepo(t, map[string]string{
		"a.txt": "1",
		"b.txt": "2",
		"c.txt": "3",
	})
	b := newGoGit(t)
	p := defaultPolicy()
	p.Clone.MaxFileCount = 2

	_, err := b.Materialize(t.Context(), attrs(repoDir, sha), p, t.TempDir())
	require.Error(t, err, "expected file-count limit error")
}

// TestGoGit_MaxRepositorySizeEnforced verifies the size limit is applied.
func TestGoGit_MaxRepositorySizeEnforced(t *testing.T) {
	repoDir, sha := newTestRepo(t, map[string]string{
		"big.txt": "AAAAAAAAAA",
	})
	b := newGoGit(t)
	p := defaultPolicy()
	p.Clone.MaxRepositorySize = 5

	_, err := b.Materialize(t.Context(), attrs(repoDir, sha), p, t.TempDir())
	require.Error(t, err, "expected size limit error")
}

// TestGoGit_RefreshReclones verifies that Refresh re-materialises from the repo.
func TestGoGit_RefreshReclones(t *testing.T) {
	repoDir, sha := newTestRepo(t, map[string]string{"v1.txt": "first"})
	b := newGoGit(t)
	workDir := t.TempDir()

	if _, err := b.Refresh(t.Context(), attrs(repoDir, sha), defaultPolicy(), workDir); err != nil {
		require.NoError(t, err, "Refresh")
	}
	assertFileContent(t, filepath.Join(workDir, "content", "v1.txt"), "first")
}

// TestSafeJoin covers path-escape and .git segment rejections.
func TestSafeJoin(t *testing.T) {
	root := "/root"
	cases := []struct {
		rel     string
		wantErr bool
	}{
		{"file.txt", false},
		{"sub/file.txt", false},
		{"../escape", true},
		{"sub/../../escape", true},
		{".git/config", true},
		{"sub/.git/config", true},
	}
	for _, tc := range cases {
		_, err := safeJoin(root, tc.rel)
		if tc.wantErr {
			require.Error(t, err, "safeJoin(%q) should error", tc.rel)
		} else {
			require.NoError(t, err, "safeJoin(%q) should not error", tc.rel)
		}
	}
}

// TestValidateSymlinkTarget covers absolute and escaping symlink detection.
func TestValidateSymlinkTarget(t *testing.T) {
	root := "/content"
	cases := []struct {
		linkPath string
		target   string
		wantErr  bool
	}{
		{"/content/link", "target.txt", false},
		{"/content/sub/link", "../sibling.txt", false},
		{"/content/link", "/etc/passwd", true},
		{"/content/link", "../../etc/passwd", true},
		{"/content/sub/link", "../../escape", true},
	}
	for _, tc := range cases {
		err := validateSymlinkTarget(root, tc.linkPath, tc.target)
		if (err != nil) != tc.wantErr {
			require.Equal(t, tc.wantErr, err != nil, "validateSymlinkTarget(%q→%q): err=%v wantErr=%v",
				tc.linkPath, tc.target, err, tc.wantErr)
		}
	}
}

// TestIsPinnedCommit checks the 40-hex detection.
func TestIsPinnedCommit(t *testing.T) {
	cases := []struct {
		rev  string
		want bool
	}{
		{"abc123" + string(make([]byte, 34)), false},
		{"0000000000000000000000000000000000000000", true},
		{"da39a3ee5e6b4b0d3255bfef95601890afd80709", true},
		{"main", false},
		{"refs/heads/main", false},
		{"DA39A3EE5E6B4B0D3255BFEF95601890AFD80709", false}, // uppercase rejected
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, isPinnedCommit(tc.rev))
	}
}

// TestMutableRef verifies ref normalisation across all supported formats.
func TestMutableRef(t *testing.T) {
	cases := []struct {
		attrs volume.Attributes
		want  string
	}{
		{volume.Attributes{Revision: "main", RevisionKind: "branch"}, "main"},
		{volume.Attributes{Revision: "refs/heads/main", RevisionKind: "branch"}, "main"},
		{volume.Attributes{Revision: "branch:main"}, "main"},
		{volume.Attributes{Revision: "v1.0", RevisionKind: "tag"}, "v1.0"},
		{volume.Attributes{Revision: "refs/tags/v1.0"}, "v1.0"},
		{volume.Attributes{Revision: "tag:v1.0"}, "v1.0"},
		{volume.Attributes{Revision: "da39a3ee5e6b4b0d3255bfef95601890afd80709"}, ""},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, mutableRef(tc.attrs))
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	require.NoError(t, err, "ReadFile(%s)", path)
	assert.Equal(t, want, string(got), "file %s: content mismatch", path)
}
