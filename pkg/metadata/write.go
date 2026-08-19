package metadata

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Inputs struct {
	Repo             string
	RequestedRev     string
	ResolvedRev      string
	Policy           string
	Submodules       bool
	CredentialProfile string
}

func Write(root string, in Inputs) error {
	meta := filepath.Join(root, ".gitcontent")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		return err
	}
	fields := map[string]string{
		"repo":               in.Repo,
		"requested-revision": in.RequestedRev,
		"resolved-revision":  in.ResolvedRev,
		"mounted-at":         time.Now().UTC().Format(time.RFC3339),
		"policy":             in.Policy,
		"submodules-enabled": fmt.Sprintf("%t", in.Submodules),
		"credential-profile": in.CredentialProfile,
	}
	for name, value := range fields {
		if err := os.WriteFile(filepath.Join(meta, name), []byte(value+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}
