package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeTargetPathAllowsPathUnderRoot(t *testing.T) {
	got, err := safeTargetPath("/var/lib/kubelet", "/var/lib/kubelet/pods/pod-id/volumes/gitcontent")
	require.NoError(t, err, "safeTargetPath returned error: %v", err)
	assert.Equal(t, "/var/lib/kubelet/pods/pod-id/volumes/gitcontent", got)
}

func TestSafeTargetPathRejectsEscapes(t *testing.T) {
	cases := []string{
		"/etc/passwd",
		"/var/lib/kubelet/../secret",
		"/var/lib/kubelet",
		"relative/path",
	}
	for _, target := range cases {
		t.Run(target, func(t *testing.T) {
			_, err := safeTargetPath("/var/lib/kubelet", target)
			require.Error(t, err, "safeTargetPath(%q) should error", target)
		})
	}
}
