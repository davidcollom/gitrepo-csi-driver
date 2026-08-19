package materializer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyContentTreeSkipsGitDirectory(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "content")

	writeTestFile(t, filepath.Join(src, "public", "index.html"), "ok\n")
	writeTestFile(t, filepath.Join(src, ".git", "config"), "secret\n")
	writeTestFile(t, filepath.Join(src, "public", ".git", "hooks", "post-checkout"), "hook\n")

	if err := CopyContentTree(src, dst); err != nil {
		t.Fatalf("CopyContentTree returned error: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dst, "public", "index.html")); err != nil || string(got) != "ok\n" {
		t.Fatalf("copied content = %q, err = %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(err) {
		t.Fatalf("expected top-level .git to be omitted, got err %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "public", ".git")); !os.IsNotExist(err) {
		t.Fatalf("expected nested .git to be omitted, got err %v", err)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
