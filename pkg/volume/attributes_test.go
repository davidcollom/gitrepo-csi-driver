package volume

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMinimal(t *testing.T) {
	attrs, err := Parse(map[string]string{
		"repo":     "https://github.com/example/repo.git",
		"revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, attrs.Repo)
	assert.NotEmpty(t, attrs.Revision)
}

func TestParseRejectsBadDepth(t *testing.T) {
	_, err := Parse(map[string]string{
		"repo":     "https://github.com/example/repo.git",
		"revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"depth":    "0",
	})
	require.Error(t, err, "depth=0 should be rejected")
}

func TestParseRejectsLocalRepository(t *testing.T) {
	cases := []string{
		"/var/lib/kubelet/pods/victim/repo",
		"../repo",
		"./repo",
		"file:///var/lib/kubelet/pods/victim/repo",
	}
	for _, repo := range cases {
		t.Run(repo, func(t *testing.T) {
			_, err := Parse(map[string]string{
				"repo":     repo,
				"revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			})
			assert.Error(t, err, "local repo %q must be rejected", repo)
		})
	}
}

func TestParseAllowsRemoteRepositoryForms(t *testing.T) {
	cases := []string{
		"https://github.com/example/repo.git",
		"http://git.example.test/repo.git",
		"ssh://git@git.example.test/platform/repo.git",
		"git@git.example.test:platform/repo.git",
	}
	for _, repo := range cases {
		t.Run(repo, func(t *testing.T) {
			_, err := Parse(map[string]string{
				"repo":     repo,
				"revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			})
			assert.NoError(t, err, "remote repo %q should be allowed", repo)
		})
	}
}

func TestParseNormalizesSafePath(t *testing.T) {
	attrs, err := Parse(map[string]string{
		"repo":     "https://github.com/example/repo.git",
		"revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"path":     "public/./assets/",
	})
	require.NoError(t, err)
	assert.Equal(t, "public/assets", attrs.Path)
}

func TestParseRejectsUnsafePath(t *testing.T) {
	cases := []string{
		"../secret",
		"/secret",
		"public/../../secret",
		".git",
		"public/.git/hooks",
		`public\.git`,
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			_, err := Parse(map[string]string{
				"repo":     "https://github.com/example/repo.git",
				"revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"path":     path,
			})
			assert.Error(t, err, "unsafe path %q must be rejected", path)
		})
	}
}

func TestParseRejectsUnsafeRevision(t *testing.T) {
	cases := []string{
		"--upload-pack=/tmp/pwn",
		"branch:--help",
		"refs/heads/../main",
		"refs/heads/main.lock",
		"refs/heads/main~1",
		"refs/heads/feature @{bad}",
	}
	for _, revision := range cases {
		t.Run(revision, func(t *testing.T) {
			_, err := Parse(map[string]string{
				"repo":     "https://github.com/example/repo.git",
				"revision": revision,
			})
			assert.Error(t, err, "unsafe revision %q must be rejected", revision)
		})
	}
}

func TestParseAllowsSafeRevision(t *testing.T) {
	cases := []string{
		"main",
		"feature/test-1",
		"refs/heads/release/v1",
		"tag:v1.2.3",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	for _, revision := range cases {
		t.Run(revision, func(t *testing.T) {
			_, err := Parse(map[string]string{
				"repo":     "https://github.com/example/repo.git",
				"revision": revision,
			})
			assert.NoError(t, err, "safe revision %q should be allowed", revision)
		})
	}
}
