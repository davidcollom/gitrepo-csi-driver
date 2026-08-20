package materializer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRejectGitFlagOperand(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "safe ref", value: "main"},
		{name: "safe repo", value: "https://github.com/example/repo.git"},
		{name: "git option injection", value: "--upload-pack=/tmp/pwn", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectGitFlagOperand(tt.value, "test operand")
			if (err != nil) != tt.wantErr {
				t.Fatalf("rejectGitFlagOperand(%q) error = %v, wantErr %t", tt.value, err, tt.wantErr)
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

func TestCopyContentTreeRejectsEscapingSymlink(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "content")

	if err := os.MkdirAll(filepath.Join(src, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../secret", filepath.Join(src, "public", "secret")); err != nil {
		t.Fatal(err)
	}

	if err := CopyContentTree(src, dst); err == nil {
		t.Fatalf("expected escaping symlink to be rejected")
	}
}

func TestCopyContentTreeCopiesSafeSymlink(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "content")

	writeTestFile(t, filepath.Join(src, "public", "target.txt"), "ok\n")
	if err := os.Symlink("target.txt", filepath.Join(src, "public", "link.txt")); err != nil {
		t.Fatal(err)
	}

	if err := CopyContentTree(src, dst); err != nil {
		t.Fatalf("CopyContentTree returned error: %v", err)
	}
	target, err := os.Readlink(filepath.Join(dst, "public", "link.txt"))
	if err != nil {
		t.Fatalf("read copied symlink: %v", err)
	}
	if target != "target.txt" {
		t.Fatalf("symlink target = %q, want target.txt", target)
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
