package policy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadPoliciesParsesDurationString(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policies.yaml")
	if err := os.WriteFile(path, []byte(`policies:
  - name: default
    allowedRepositories:
      - https://github.com/example/*
    allowedHosts:
      - github.com
    clone:
      timeout: 60s
`), 0o644); err != nil {
		t.Fatal(err)
	}

	policies, err := LoadPolicies(path)
	if err != nil {
		t.Fatalf("LoadPolicies returned error: %v", err)
	}
	if got := policies[0].Clone.Timeout; got != time.Minute {
		t.Fatalf("timeout = %s, want 1m0s", got)
	}
}
