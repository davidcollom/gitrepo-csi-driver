package policy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPoliciesParsesDurationString(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policies.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`policies:
  - name: default
    allowedRepositories:
      - https://github.com/example/*
    allowedHosts:
      - github.com
    clone:
      timeout: 60s
`), 0o644))

	policies, err := LoadPolicies(path)
	require.NoError(t, err)
	assert.Equal(t, time.Minute, policies[0].Clone.Timeout)
}
