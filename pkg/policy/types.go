package policy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// GitContentPolicy models policy controls from the RFC.
type GitContentPolicy struct {
	Name                string            `yaml:"name" json:"name"`
	AllowedRepositories []string          `yaml:"allowedRepositories" json:"allowedRepositories"`
	AllowedHosts        []string          `yaml:"allowedHosts" json:"allowedHosts"`
	Revisions           RevisionPolicy    `yaml:"revisions" json:"revisions"`
	Clone               ClonePolicy       `yaml:"clone" json:"clone"`
	Submodules          SubmodulePolicy   `yaml:"submodules" json:"submodules"`
	LFS                 LFSPolicy         `yaml:"lfs" json:"lfs"`
	Credentials         CredentialPolicy  `yaml:"credentials" json:"credentials"`
	Namespaces          []string          `yaml:"namespaces" json:"namespaces"`
	Labels              map[string]string `yaml:"labels" json:"labels"`
	Annotations         map[string]string `yaml:"annotations" json:"annotations"`
}

type RevisionPolicy struct {
	RequirePinnedCommit bool     `yaml:"requirePinnedCommit" json:"requirePinnedCommit"`
	AllowBranches       bool     `yaml:"allowBranches" json:"allowBranches"`
	AllowTags           bool     `yaml:"allowTags" json:"allowTags"`
	AllowedBranchRegex  []string `yaml:"allowedBranchPatterns" json:"allowedBranchPatterns"`
	AllowedTagRegex     []string `yaml:"allowedTagPatterns" json:"allowedTagPatterns"`
}

type ClonePolicy struct {
	DefaultDepth        int           `yaml:"defaultDepth" json:"defaultDepth"`
	MaxDepth            int           `yaml:"maxDepth" json:"maxDepth"`
	Timeout             time.Duration `yaml:"timeout" json:"timeout"`
	MaxRepositorySize   int64         `yaml:"maxRepositorySize" json:"maxRepositorySize"`
	MaxFileCount        int64         `yaml:"maxFileCount" json:"maxFileCount"`
	AllowSparseCheckout bool          `yaml:"allowSparseCheckout" json:"allowSparseCheckout"`
}

func (p *ClonePolicy) UnmarshalJSON(data []byte) error {
	type clonePolicy ClonePolicy
	var raw struct {
		clonePolicy
		Timeout any `json:"timeout"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*p = ClonePolicy(raw.clonePolicy)
	timeout, err := parseDuration(raw.Timeout)
	if err != nil {
		return err
	}
	p.Timeout = timeout
	return nil
}

func parseDuration(raw any) (time.Duration, error) {
	switch v := raw.(type) {
	case nil:
		return 0, nil
	case string:
		if v == "" {
			return 0, nil
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, fmt.Errorf("parse clone timeout %q: %w", v, err)
		}
		return d, nil
	case float64:
		return time.Duration(v), nil
	case json.Number:
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse clone timeout %q: %w", v, err)
		}
		return time.Duration(n), nil
	default:
		return 0, fmt.Errorf("clone timeout must be a duration string or integer nanoseconds")
	}
}

type SubmodulePolicy struct {
	Enabled             bool     `yaml:"enabled" json:"enabled"`
	Recursive           bool     `yaml:"recursive" json:"recursive"`
	MaxDepth            int      `yaml:"maxDepth" json:"maxDepth"`
	AllowedRepositories []string `yaml:"allowedRepositories" json:"allowedRepositories"`
	AllowedHosts        []string `yaml:"allowedHosts" json:"allowedHosts"`
}

type LFSPolicy struct {
	Enabled       bool  `yaml:"enabled" json:"enabled"`
	MaxObjectSize int64 `yaml:"maxObjectSize" json:"maxObjectSize"`
	MaxTotalSize  int64 `yaml:"maxTotalSize" json:"maxTotalSize"`
}

type CredentialPolicy struct {
	DefaultProfile  string   `yaml:"defaultProfile" json:"defaultProfile"`
	AllowedProfiles []string `yaml:"allowedProfiles" json:"allowedProfiles"`
}

type GitCredentialProfile struct {
	Name                string   `yaml:"name" json:"name"`
	Type                string   `yaml:"type" json:"type"`
	SecretRef           string   `yaml:"secretRef" json:"secretRef"`
	AllowedRepositories []string `yaml:"allowedRepositories" json:"allowedRepositories"`
}

type Request struct {
	Namespace         string
	Repo              string
	Revision          string
	Path              string
	Depth             int
	Submodules        bool
	LFS               bool
	CredentialProfile string
}

type Result struct {
	Allowed          bool
	ResolvedProfile  string
	DenialReasonCode string
	DenialReason     string
}
