package main

import "testing"

func TestSafeTargetPathAllowsPathUnderRoot(t *testing.T) {
	got, err := safeTargetPath("/var/lib/kubelet", "/var/lib/kubelet/pods/pod-id/volumes/gitcontent")
	if err != nil {
		t.Fatalf("safeTargetPath returned error: %v", err)
	}
	if got != "/var/lib/kubelet/pods/pod-id/volumes/gitcontent" {
		t.Fatalf("target = %q", got)
	}
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
			if _, err := safeTargetPath("/var/lib/kubelet", target); err == nil {
				t.Fatalf("expected target path validation error")
			}
		})
	}
}
