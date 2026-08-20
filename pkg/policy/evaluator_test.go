package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvaluatePinnedRevisionRequired(t *testing.T) {
	e := Evaluator{Policies: []GitContentPolicy{{
		Name:                "default",
		Namespaces:          []string{"web"},
		AllowedRepositories: []string{"https://github.com/example/*"},
		AllowedHosts:        []string{"github.com"},
		Revisions: RevisionPolicy{
			RequirePinnedCommit: true,
		},
		Credentials: CredentialPolicy{
			DefaultProfile:  "github-readonly",
			AllowedProfiles: []string{"github-readonly"},
		},
	}}}

	res, _ := e.Evaluate(Request{
		Namespace: "web",
		Repo:      "https://github.com/example/site.git",
		Revision:  "refs/tags/v1.0.0",
	}, "")
	assert.False(t, res.Allowed, "non-SHA revision must be denied")
}

func TestEvaluateAllowed(t *testing.T) {
	e := Evaluator{Policies: []GitContentPolicy{{
		Name:                "default",
		AllowedRepositories: []string{"https://github.com/example/*"},
		AllowedHosts:        []string{"github.com"},
		Revisions: RevisionPolicy{
			RequirePinnedCommit: false,
			AllowTags:           true,
		},
		Credentials: CredentialPolicy{
			DefaultProfile:  "github-readonly",
			AllowedProfiles: []string{"github-readonly"},
		},
	}}}

	res, _ := e.Evaluate(Request{
		Namespace: "web",
		Repo:      "https://github.com/example/site.git",
		Revision:  "refs/tags/v1.0.0",
	}, "")
	assert.True(t, res.Allowed, "expected allow, got deny: %s", res.DenialReason)
}
