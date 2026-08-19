package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/cache"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/materializer"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/metadata"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/observability"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/policy"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/volume"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const driverName = "gitcontent.csi.example.io"

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

type mountPipeline struct {
	evaluator policy.Evaluator
	mat       materializer.Backend
	metrics   *observability.Metrics
	cacheMgr  *cache.Manager
}

type mountResult struct {
	ResolvedRevision string
	Policy           string
	SourcePath       string
}

type server struct {
	csi.UnimplementedIdentityServer
	csi.UnimplementedNodeServer

	nodeID   string
	pipeline *mountPipeline
}

func main() {
	policyPath := getenv("GITCONTENT_POLICY_FILE", "./examples/policies.yaml")
	cacheDir := getenv("GITCONTENT_CACHE_DIR", "/tmp/gitcontent-cache")
	httpAddr := getenv("GITCONTENT_ADDR", ":8080")
	endpoint := getenv("CSI_ENDPOINT", "unix:///csi/csi.sock")
	nodeID := getenv("NODE_ID", hostname())

	policies, err := policy.LoadPolicies(policyPath)
	if err != nil {
		panic(err)
	}
	metrics := observability.NewMetrics()
	metrics.Register()

	cacheMgr, err := cache.New(cache.Config{RootDir: cacheDir, MaxSize: 10 * 1024 * 1024 * 1024, MaxAge: 24 * time.Hour})
	if err != nil {
		panic(err)
	}

	pipeline := &mountPipeline{
		evaluator: policy.Evaluator{Policies: policies},
		mat:       materializer.New(),
		metrics:   metrics,
		cacheMgr:  cacheMgr,
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- serveHTTP(httpAddr, metrics, pipeline)
	}()
	go func() {
		errCh <- serveCSI(endpoint, &server{nodeID: nodeID, pipeline: pipeline})
	}()

	if err := <-errCh; err != nil {
		panic(err)
	}
}

func serveHTTP(addr string, metrics *observability.Metrics, pipeline *mountPipeline) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/mount", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req mountRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			write(w, http.StatusBadRequest, mountResponse{Success: false, Message: err.Error()})
			return
		}

		res, err := pipeline.materialize(r.Context(), req.Namespace, req.TargetPath, req.VolumeAttributes)
		if err != nil {
			statusCode := http.StatusBadGateway
			if errors.Is(err, context.DeadlineExceeded) {
				statusCode = http.StatusGatewayTimeout
			}
			var mountErr *mountError
			if errors.As(err, &mountErr) {
				statusCode = mountErr.httpStatus
			}
			write(w, statusCode, mountResponse{Success: false, Message: err.Error()})
			return
		}
		write(w, http.StatusOK, mountResponse{Success: true, Message: "mounted", ResolvedRevision: res.ResolvedRevision, Policy: res.Policy})
	})
	return http.ListenAndServe(addr, mux)
}

func serveCSI(endpoint string, srv *server) error {
	network, address, err := parseEndpoint(endpoint)
	if err != nil {
		return err
	}
	if network == "unix" {
		if err := os.MkdirAll(filepath.Dir(address), 0o755); err != nil {
			return err
		}
		_ = os.Remove(address)
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		return err
	}
	log.Printf("serving CSI endpoint on %s", endpoint)

	grpcServer := grpc.NewServer()
	csi.RegisterIdentityServer(grpcServer, srv)
	csi.RegisterNodeServer(grpcServer, srv)
	return grpcServer.Serve(listener)
}

func parseEndpoint(endpoint string) (string, string, error) {
	if strings.HasPrefix(endpoint, "unix://") {
		return "unix", strings.TrimPrefix(endpoint, "unix://"), nil
	}
	if strings.HasPrefix(endpoint, "tcp://") {
		return "tcp", strings.TrimPrefix(endpoint, "tcp://"), nil
	}
	return "", "", fmt.Errorf("unsupported CSI endpoint %q", endpoint)
}

func (s *server) GetPluginInfo(context.Context, *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          driverName,
		VendorVersion: "dev",
	}, nil
}

func (s *server) GetPluginCapabilities(context.Context, *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
		},
	}, nil
}

func (s *server) Probe(context.Context, *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}

func (s *server) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}
	if req.GetTargetPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_path is required")
	}
	if req.GetVolumeCapability() == nil || req.GetVolumeCapability().GetMount() == nil {
		return nil, status.Error(codes.InvalidArgument, "only mount volume capability is supported")
	}
	if !req.GetReadonly() {
		return nil, status.Error(codes.InvalidArgument, "git content volumes must be readonly")
	}

	namespace := req.GetVolumeContext()["csi.storage.k8s.io/pod.namespace"]
	if namespace == "" {
		namespace = "default"
	}

	res, err := s.pipeline.materialize(ctx, namespace, "", req.GetVolumeContext())
	if err != nil {
		return nil, csiError(err)
	}
	if err := publishReadOnly(res.SourcePath, req.GetTargetPath()); err != nil {
		return nil, status.Errorf(codes.Internal, "publish readonly bind mount: %v", err)
	}
	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *server) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if req.GetTargetPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_path is required")
	}
	if err := unpublish(req.GetTargetPath()); err != nil {
		return nil, status.Errorf(codes.Internal, "unpublish volume: %v", err)
	}
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (s *server) NodeGetCapabilities(context.Context, *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{}, nil
}

func (s *server) NodeGetInfo(context.Context, *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{NodeId: s.nodeID}, nil
}

func (p *mountPipeline) materialize(ctx context.Context, namespace, targetPath string, rawAttrs map[string]string) (mountResult, error) {
	start := time.Now()
	attrs, err := volume.Parse(rawAttrs)
	if err != nil {
		return mountResult{}, badRequest(err)
	}

	res, pol := p.evaluator.Evaluate(policy.Request{
		Namespace:         namespace,
		Repo:              attrs.Repo,
		Revision:          attrs.Revision,
		Path:              attrs.Path,
		Depth:             attrs.Depth,
		Submodules:        attrs.Submodules,
		LFS:               attrs.LFS,
		CredentialProfile: attrs.CredentialProfile,
	}, attrs.Policy)
	if !res.Allowed {
		p.metrics.ObserveMount(namespace, pol.Name, "denied", time.Since(start))
		p.metrics.PolicyDenials.WithLabelValues(namespace, pol.Name, res.DenialReasonCode).Inc()
		return mountResult{}, forbidden(errors.New(res.DenialReason))
	}

	mountCtx, cancel := context.WithTimeout(ctx, pol.Clone.Timeout)
	defer cancel()

	cacheKey := p.cacheMgr.Key(attrs.Repo, attrs.Revision, attrs.Path, fmt.Sprintf("submodules=%t", attrs.Submodules), fmt.Sprintf("lfs=%t", attrs.LFS), res.ResolvedProfile)
	cachePath := p.cacheMgr.PathForKey(cacheKey)
	if _, err := os.Stat(cachePath); err == nil {
		p.metrics.CacheHits.WithLabelValues(policyHost(attrs.Repo)).Inc()
	} else {
		p.metrics.CacheMisses.WithLabelValues(policyHost(attrs.Repo)).Inc()
	}

	mr, err := p.mat.Materialize(mountCtx, attrs, pol, cachePath)
	if err != nil {
		p.metrics.ObserveMount(namespace, pol.Name, "error", time.Since(start))
		if errors.Is(mountCtx.Err(), context.DeadlineExceeded) {
			return mountResult{}, context.DeadlineExceeded
		}
		return mountResult{}, err
	}

	p.metrics.RepositorySize.WithLabelValues(policyHost(attrs.Repo)).Set(float64(mr.SizeBytes))
	p.metrics.RepositoryFiles.WithLabelValues(policyHost(attrs.Repo)).Set(float64(mr.FileCount))

	if err := metadata.Write(mr.MountedPath, metadata.Inputs{
		Repo:              attrs.Repo,
		RequestedRev:      attrs.Revision,
		ResolvedRev:       mr.ResolvedRevision,
		Policy:            pol.Name,
		Submodules:        attrs.Submodules,
		CredentialProfile: res.ResolvedProfile,
	}); err != nil {
		p.metrics.ObserveMount(namespace, pol.Name, "error", time.Since(start))
		return mountResult{}, err
	}

	if targetPath != "" {
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return mountResult{}, err
		}
		_ = os.RemoveAll(targetPath)
		if err := os.Symlink(mr.MountedPath, targetPath); err != nil {
			return mountResult{}, err
		}
	}

	_ = p.cacheMgr.Evict()
	p.metrics.ObserveMount(namespace, pol.Name, "success", time.Since(start))
	return mountResult{ResolvedRevision: mr.ResolvedRevision, Policy: pol.Name, SourcePath: mr.MountedPath}, nil
}

type mountError struct {
	httpStatus int
	err        error
}

func (e *mountError) Error() string {
	return e.err.Error()
}

func (e *mountError) Unwrap() error {
	return e.err
}

func badRequest(err error) error {
	return &mountError{httpStatus: http.StatusBadRequest, err: err}
}

func forbidden(err error) error {
	return &mountError{httpStatus: http.StatusForbidden, err: err}
}

func csiError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, err.Error())
	}
	var mountErr *mountError
	if errors.As(err, &mountErr) {
		switch mountErr.httpStatus {
		case http.StatusBadRequest:
			return status.Error(codes.InvalidArgument, mountErr.Error())
		case http.StatusForbidden:
			return status.Error(codes.PermissionDenied, mountErr.Error())
		}
	}
	return status.Error(codes.Internal, err.Error())
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

func hostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "unknown"
	}
	return name
}

func policyHost(repo string) string {
	h := policy.HostFromRepo(repo)
	if h == "" {
		return "unknown"
	}
	return h
}
