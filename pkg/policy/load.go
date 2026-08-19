package policy

import (
	"fmt"
	"os"
	"time"

	"sigs.k8s.io/yaml"
)

type policyDoc struct {
	Policies []GitContentPolicy `yaml:"policies"`
}

func LoadPolicies(path string) ([]GitContentPolicy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policies: %w", err)
	}

	var doc policyDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse policies: %w", err)
	}

	for i := range doc.Policies {
		applyPolicyDefaults(&doc.Policies[i])
	}

	return doc.Policies, nil
}

func applyPolicyDefaults(p *GitContentPolicy) {
	if p.Clone.DefaultDepth <= 0 {
		p.Clone.DefaultDepth = 1
	}
	if p.Clone.MaxDepth <= 0 {
		p.Clone.MaxDepth = 10
	}
	if p.Clone.Timeout <= 0 {
		p.Clone.Timeout = 30 * time.Second
	}
	if p.Clone.MaxRepositorySize <= 0 {
		p.Clone.MaxRepositorySize = 100 * 1024 * 1024
	}
	if p.Clone.MaxFileCount <= 0 {
		p.Clone.MaxFileCount = 5000
	}
	if p.Submodules.MaxDepth <= 0 {
		p.Submodules.MaxDepth = 1
	}
}
