package volume

import (
	"fmt"
	"net/url"
	"path"
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
	if err := validateRepo(out.Repo); err != nil {
		return Attributes{}, err
	}
	normalizedPath, err := normalizeRepoPath(out.Path)
	if err != nil {
		return Attributes{}, err
	}
	out.Path = normalizedPath

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

func validateRepo(repo string) error {
	if strings.ContainsAny(repo, " \t\r\n") {
		return fmt.Errorf("volume attribute repo must not contain whitespace")
	}
	if strings.HasPrefix(repo, "file://") || strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "./") || strings.HasPrefix(repo, "../") {
		return fmt.Errorf("volume attribute repo must be a remote http, https, or ssh repository URL")
	}
	if strings.Contains(repo, "://") {
		u, err := url.Parse(repo)
		if err != nil || u.Hostname() == "" {
			return fmt.Errorf("volume attribute repo must be a valid remote repository URL")
		}
		switch u.Scheme {
		case "http", "https", "ssh":
			return nil
		default:
			return fmt.Errorf("volume attribute repo scheme %q is not supported", u.Scheme)
		}
	}
	if at := strings.Index(repo, "@"); at > 0 {
		after := repo[at+1:]
		if colon := strings.Index(after, ":"); colon > 0 && colon < len(after)-1 {
			return nil
		}
	}
	return fmt.Errorf("volume attribute repo must be a remote http, https, or ssh repository URL")
}

func normalizeRepoPath(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if strings.Contains(raw, "\\") {
		return "", fmt.Errorf("volume attribute path must use forward slash separators")
	}
	if path.IsAbs(raw) {
		return "", fmt.Errorf("volume attribute path must be relative")
	}

	cleaned := path.Clean(raw)
	if cleaned == "." {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("volume attribute path must stay within the repository")
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".git" {
			return "", fmt.Errorf("volume attribute path must not reference .git")
		}
	}
	return cleaned, nil
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
