package volume

import (
	"fmt"
	"strconv"
	"strings"
)

// Attributes are CSI volumeAttributes semantics from the RFC.
type Attributes struct {
	Repo              string
	Revision          string
	RevisionKind      string
	Path              string
	Depth             int
	Submodules        bool
	LFS               bool
	Policy            string
	CredentialProfile string
	RefreshSeconds    int
}

func Parse(raw map[string]string) (Attributes, error) {
	var out Attributes

	out.Repo = strings.TrimSpace(raw["repo"])
	out.Revision = strings.TrimSpace(raw["revision"])
	out.RevisionKind = strings.TrimSpace(raw["revisionKind"])
	out.Path = strings.TrimSpace(raw["path"])
	out.Policy = strings.TrimSpace(raw["policy"])
	out.CredentialProfile = strings.TrimSpace(raw["credentialProfile"])

	if out.Repo == "" {
		return Attributes{}, fmt.Errorf("volume attribute repo is required")
	}
	if out.Revision == "" {
		return Attributes{}, fmt.Errorf("volume attribute revision is required")
	}

	depthRaw := strings.TrimSpace(raw["depth"])
	if depthRaw != "" {
		n, err := strconv.Atoi(depthRaw)
		if err != nil || n <= 0 {
			return Attributes{}, fmt.Errorf("volume attribute depth must be a positive integer")
		}
		out.Depth = n
	}

	submodulesRaw := strings.TrimSpace(raw["submodules"])
	if submodulesRaw != "" {
		b, err := parseBool(submodulesRaw)
		if err != nil {
			return Attributes{}, fmt.Errorf("volume attribute submodules must be true|false")
		}
		out.Submodules = b
	}

	lfsRaw := strings.TrimSpace(raw["lfs"])
	if lfsRaw != "" {
		b, err := parseBool(lfsRaw)
		if err != nil {
			return Attributes{}, fmt.Errorf("volume attribute lfs must be true|false")
		}
		out.LFS = b
	}

	refreshRaw := strings.TrimSpace(raw["refreshSeconds"])
	if refreshRaw != "" {
		n, err := strconv.Atoi(refreshRaw)
		if err != nil || n < 0 {
			return Attributes{}, fmt.Errorf("volume attribute refreshSeconds must be a non-negative integer")
		}
		out.RefreshSeconds = n
	}

	return out, nil
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool")
	}
}
