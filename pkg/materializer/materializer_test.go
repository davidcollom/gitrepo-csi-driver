package materializer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRejectGitFlagOperand(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "safe ref", value: "main"},
		{name: "safe repo", value: "https://github.com/example/repo.git"},
		{name: "git option injection", value: "--upload-pack=/tmp/pwn", wantErr: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectGitFlagOperand(tt.value, "test operand")
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCopyContentTreeSkipsGitDirectory(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "content")

	writeTestFile(t, filepath.Join(src, "public", "index.html"), "ok\n")
	writeTestFile(t, filepath.Join(src, ".git", "config"), "secret\n")
	writeTestFile(t, filepath.Join(src, "public", ".git", "hooks", "post-checkout"), "hook\n")

	require.NoError(t, CopyContentTree(src, dst))

	got, err := os.ReadFile(filepath.Join(dst, "public", "index.html"))
	require.NoError(t, err)
	assert.Equal(t, "ok\n", string(got))

	_, err = os.Stat(filepath.Join(dst, ".git"))
	assert.True(t, os.IsNotExist(err), "top-level .git must be omitted")

	_, err = os.Stat(filepath.Join(dst, "public", ".git"))
	assert.True(t, os.IsNotExist(err), "nested .git must be omitted")
}

func TestCopyContentTreeRejectsEscapingSymlink(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "content")

	require.NoError(t, os.MkdirAll(filepath.Join(src, "public"), 0o755))
	require.NoError(t, os.Symlink("../../secret", filepath.Join(src, "public", "secret")))

	require.Error(t, CopyContentTree(src, dst), "escaping symlink must be rejected")
}

func TestCopyContentTreeCopiesSafeSymlink(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "content")

	writeTestFile(t, filepath.Join(src, "public", "target.txt"), "ok\n")
	require.NoError(t, os.Symlink("target.txt", filepath.Join(src, "public", "link.txt")))

	require.NoError(t, CopyContentTree(src, dst))

	target, err := os.Readlink(filepath.Join(dst, "public", "link.txt"))
	require.NoError(t, err)
	assert.Equal(t, "target.txt", target)
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}
