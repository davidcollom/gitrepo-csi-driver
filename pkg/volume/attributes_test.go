package volume

import "testing"

func TestParseMinimal(t *testing.T) {
	attrs, err := Parse(map[string]string{
		"repo":     "https://github.com/example/repo.git",
		"revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attrs.Repo == "" || attrs.Revision == "" {
		t.Fatalf("expected required fields")
	}
}

func TestParseRejectsBadDepth(t *testing.T) {
	_, err := Parse(map[string]string{
		"repo":     "https://github.com/example/repo.git",
		"revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"depth":    "0",
	})
	if err == nil {
		t.Fatalf("expected depth validation error")
	}
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
			if err == nil {
				t.Fatalf("expected repo validation error")
			}
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
			if err != nil {
				t.Fatalf("unexpected repo validation error: %v", err)
			}
		})
	}
}

func TestParseNormalizesSafePath(t *testing.T) {
	attrs, err := Parse(map[string]string{
		"repo":     "https://github.com/example/repo.git",
		"revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"path":     "public/./assets/",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attrs.Path != "public/assets" {
		t.Fatalf("path = %q, want public/assets", attrs.Path)
	}
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
			if err == nil {
				t.Fatalf("expected path validation error")
			}
		})
	}
}
