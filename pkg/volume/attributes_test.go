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
