package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/davidcollom/gitrepo-csi-driver/pkg/cache"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/materializer"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/metadata"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/observability"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/policy"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/volume"
)

type mountRequest struct {
	Namespace        string            `json:"namespace"`
	TargetPath       string            `json:"targetPath"`
	VolumeAttributes map[string]string `json:"volumeAttributes"`
}

type mountResponse struct {
	Success          bool   `json:"success"`
	Message          string `json:"message"`
	ResolvedRevision string `json:"resolvedRevision,omitempty"`
	Policy           string `json:"policy,omitempty"`
}

func main() {
	policyPath := getenv("GITCONTENT_POLICY_FILE", "./examples/policies.yaml")
	cacheDir := getenv("GITCONTENT_CACHE_DIR", "/tmp/gitcontent-cache")
	addr := getenv("GITCONTENT_ADDR", ":8080")

	policies, err := policy.LoadPolicies(policyPath)
	if err != nil {
		panic(err)
	}
	evaluator := policy.Evaluator{Policies: policies}
	mat := materializer.New()
	metrics := observability.NewMetrics()
	metrics.Register()

	cacheMgr, err := cache.New(cache.Config{RootDir: cacheDir, MaxSize: 10 * 1024 * 1024 * 1024, MaxAge: 24 * time.Hour})
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/mount", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req mountRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			write(w, http.StatusBadRequest, mountResponse{Success: false, Message: err.Error()})
			return
		}

		attrs, err := volume.Parse(req.VolumeAttributes)
		if err != nil {
			write(w, http.StatusBadRequest, mountResponse{Success: false, Message: err.Error()})
			return
		}

		res, pol := evaluator.Evaluate(policy.Request{
			Namespace:         req.Namespace,
			Repo:              attrs.Repo,
			Revision:          attrs.Revision,
			Path:              attrs.Path,
			Depth:             attrs.Depth,
			Submodules:        attrs.Submodules,
			LFS:               attrs.LFS,
			CredentialProfile: attrs.CredentialProfile,
		}, attrs.Policy)
		if !res.Allowed {
			metrics.ObserveMount(req.Namespace, pol.Name, "denied", time.Since(start))
			metrics.PolicyDenials.WithLabelValues(req.Namespace, pol.Name, res.DenialReasonCode).Inc()
			write(w, http.StatusForbidden, mountResponse{Success: false, Message: res.DenialReason})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), pol.Clone.Timeout)
		defer cancel()

		cacheKey := cacheMgr.Key(attrs.Repo, attrs.Revision, attrs.Path, fmt.Sprintf("submodules=%t", attrs.Submodules), fmt.Sprintf("lfs=%t", attrs.LFS), res.ResolvedProfile)
		cachePath := cacheMgr.PathForKey(cacheKey)
		if _, err := os.Stat(cachePath); err == nil {
			metrics.CacheHits.WithLabelValues(policyHost(attrs.Repo)).Inc()
		} else {
			metrics.CacheMisses.WithLabelValues(policyHost(attrs.Repo)).Inc()
		}

		mr, err := mat.Materialize(ctx, attrs, pol, cachePath)
		if err != nil {
			status := http.StatusBadGateway
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				status = http.StatusGatewayTimeout
			}
			metrics.ObserveMount(req.Namespace, pol.Name, "error", time.Since(start))
			write(w, status, mountResponse{Success: false, Message: err.Error()})
			return
		}

		metrics.RepositorySize.WithLabelValues(policyHost(attrs.Repo)).Set(float64(mr.SizeBytes))
		metrics.RepositoryFiles.WithLabelValues(policyHost(attrs.Repo)).Set(float64(mr.FileCount))

		if err := metadata.Write(mr.MountedPath, metadata.Inputs{
			Repo:              attrs.Repo,
			RequestedRev:      attrs.Revision,
			ResolvedRev:       mr.ResolvedRevision,
			Policy:            pol.Name,
			Submodules:        attrs.Submodules,
			CredentialProfile: res.ResolvedProfile,
		}); err != nil {
			metrics.ObserveMount(req.Namespace, pol.Name, "error", time.Since(start))
			write(w, http.StatusInternalServerError, mountResponse{Success: false, Message: err.Error()})
			return
		}

		if req.TargetPath != "" {
			if err := os.MkdirAll(filepath.Dir(req.TargetPath), 0o755); err != nil {
				write(w, http.StatusInternalServerError, mountResponse{Success: false, Message: err.Error()})
				return
			}
			_ = os.RemoveAll(req.TargetPath)
			if err := os.Symlink(mr.MountedPath, req.TargetPath); err != nil {
				write(w, http.StatusInternalServerError, mountResponse{Success: false, Message: err.Error()})
				return
			}
		}

		_ = cacheMgr.Evict()
		metrics.ObserveMount(req.Namespace, pol.Name, "success", time.Since(start))
		write(w, http.StatusOK, mountResponse{Success: true, Message: "mounted", ResolvedRevision: mr.ResolvedRevision, Policy: pol.Name})
	})

	if err := http.ListenAndServe(addr, mux); err != nil {
		panic(err)
	}
}

func write(w http.ResponseWriter, status int, body mountResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func getenv(name, fallback string) string {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	return v
}

func policyHost(repo string) string {
	h := policy.HostFromRepo(repo)
	if h == "" {
		return "unknown"
	}
	return h
}
