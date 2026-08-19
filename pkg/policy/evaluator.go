package policy

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

type Evaluator struct {
	Policies []GitContentPolicy
}

func (e Evaluator) Evaluate(req Request, policyName string) (Result, GitContentPolicy) {
	p, ok := e.selectPolicy(req.Namespace, policyName)
	if !ok {
		return denied("policy_not_found", "no matching policy found"), GitContentPolicy{}
	}

	if !matchesAnyGlob(p.AllowedRepositories, req.Repo) {
		return denied("repo_not_allowed", fmt.Sprintf("repository %s is not allowed by policy %s", req.Repo, p.Name)), p
	}

	host := HostFromRepo(req.Repo)
	if !contains(p.AllowedHosts, host) {
		return denied("host_not_allowed", fmt.Sprintf("git host %s is not allowed by policy %s", host, p.Name)), p
	}

	if req.Submodules && !p.Submodules.Enabled {
		return denied("submodules_disabled", "submodules were requested but are disabled by policy"), p
	}
	if req.LFS && !p.LFS.Enabled {
		return denied("lfs_disabled", "git lfs was requested but is disabled by policy"), p
	}

	if req.Depth > 0 && p.Clone.MaxDepth > 0 && req.Depth > p.Clone.MaxDepth {
		return denied("depth_exceeded", fmt.Sprintf("requested depth %d exceeds policy max depth %d", req.Depth, p.Clone.MaxDepth)), p
	}

	if err := checkRevisionPolicy(req.Revision, p.Revisions); err != nil {
		return denied("revision_denied", err.Error()), p
	}

	profile := req.CredentialProfile
	if profile == "" {
		profile = p.Credentials.DefaultProfile
	}
	if profile != "" && !contains(p.Credentials.AllowedProfiles, profile) {
		return denied("credential_profile_denied", fmt.Sprintf("credential profile %s is not allowed by policy %s", profile, p.Name)), p
	}

	return Result{Allowed: true, ResolvedProfile: profile}, p
}

func checkRevisionPolicy(rev string, rp RevisionPolicy) error {
	isCommit := isCommitSHA(rev)
	isTag := strings.HasPrefix(rev, "refs/tags/") || strings.HasPrefix(rev, "tag:")
	isBranch := strings.HasPrefix(rev, "refs/heads/") || strings.HasPrefix(rev, "branch:") || (!isCommit && !isTag)

	if rp.RequirePinnedCommit && !isCommit {
		return fmt.Errorf("revision must be a pinned commit SHA")
	}
	if isBranch && !rp.AllowBranches && !rp.RequirePinnedCommit {
		return fmt.Errorf("branch revisions are not allowed by policy")
	}
	if isTag && !rp.AllowTags && !rp.RequirePinnedCommit {
		return fmt.Errorf("tag revisions are not allowed by policy")
	}
	if isBranch && len(rp.AllowedBranchRegex) > 0 {
		if !matchesAnyRegex(rp.AllowedBranchRegex, rev) {
			return fmt.Errorf("branch does not match allowed branch patterns")
		}
	}
	if isTag && len(rp.AllowedTagRegex) > 0 {
		if !matchesAnyRegex(rp.AllowedTagRegex, rev) {
			return fmt.Errorf("tag does not match allowed tag patterns")
		}
	}
	return nil
}

func (e Evaluator) selectPolicy(ns string, name string) (GitContentPolicy, bool) {
	for _, p := range e.Policies {
		if name != "" {
			if p.Name != name {
				continue
			}
			if len(p.Namespaces) == 0 || contains(p.Namespaces, ns) {
				return p, true
			}
			continue
		}
		if len(p.Namespaces) == 0 || contains(p.Namespaces, ns) {
			return p, true
		}
	}
	return GitContentPolicy{}, false
}

func HostFromRepo(repo string) string {
	if strings.HasPrefix(repo, "ssh://") || strings.HasPrefix(repo, "http://") || strings.HasPrefix(repo, "https://") {
		u, err := url.Parse(repo)
		if err == nil {
			return u.Hostname()
		}
	}
	// Handles scp-like form git@host:org/repo.git
	if at := strings.Index(repo, "@"); at != -1 {
		after := repo[at+1:]
		if c := strings.Index(after, ":"); c != -1 {
			return after[:c]
		}
	}
	return ""
}

func matchesAnyGlob(patterns []string, value string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, p := range patterns {
		ok, _ := filepath.Match(p, value)
		if ok {
			return true
		}
	}
	return false
}

func matchesAnyRegex(patterns []string, value string) bool {
	for _, raw := range patterns {
		re, err := regexp.Compile(raw)
		if err != nil {
			continue
		}
		if re.MatchString(value) {
			return true
		}
	}
	return false
}

func contains(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func denied(code string, msg string) Result {
	return Result{Allowed: false, DenialReasonCode: code, DenialReason: msg}
}

func isCommitSHA(rev string) bool {
	if len(rev) != 40 {
		return false
	}
	for _, ch := range rev {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}
