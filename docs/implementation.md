# RFC Implementation Coverage

This document maps the RFC MVP scope to concrete implementation in this repository.

## Implemented MVP Features

### CSI-style Ephemeral Read-only Volume Request Flow

- Implemented as a CSI gRPC node service in `cmd/nodeplugin/main.go`.
- `NodePublishVolume` handles inline ephemeral CSI volumes from kubelet, enforces `readOnly`, evaluates policy, materializes content, and publishes a read-only content copy at kubelet's target path.
- The node plugin also keeps the HTTP `/mount` path for local/debug materialization through the same policy and materializer pipeline.

### HTTPS and SSH Repository URL Support

- Volume parsing accepts remote `http://`, `https://`, `ssh://`, and scp-like SSH repository forms in `pkg/volume/attributes.go`.
- Local filesystem paths and `file://` repositories are rejected before materialization to address the CVE-2025-1767 class of node-local repository access.
- URL handling supports `https://`, `ssh://`, and scp-like host extraction in `pkg/policy/evaluator.go`.
- Materialization executes standard Git clone and checkout operations in `pkg/materializer/materializer.go`.

### Pinned Commit, Tag, and Branch Policy Controls

- Revision controls implemented in `pkg/policy/evaluator.go`.
- `requirePinnedCommit`, `allowBranches`, `allowTags`, and regex pattern checks are enforced.

### Shallow Clone and Optional Path Selection

- Depth defaults and max depth constraints are policy-driven in `pkg/policy/load.go` and enforced in `pkg/policy/evaluator.go`.
- Clone depth is applied during materialization and optional `path` selection is supported in `pkg/materializer/materializer.go`.
- Volume `path` values are normalized as repository-relative paths in `pkg/volume/attributes.go`; absolute paths, traversal outside the repository, backslash separators, and `.git` path segments are rejected.

### Submodules Disabled by Default, Optional Policy-Gated Support

- `submodules=false` default parsing in `pkg/volume/attributes.go`.
- Policy gating in `pkg/policy/evaluator.go`.
- Non-recursive submodule init path implemented in `pkg/materializer/materializer.go`.

### Repository and Host Allowlists

- Enforced in `pkg/policy/evaluator.go` using repository glob patterns and explicit host allowlists.

### Credential Profile Governance

- Requested/default profile selection and allowlist checks in `pkg/policy/evaluator.go`.
- Profile fields modeled in `pkg/policy/types.go`.

### Clone Timeout, Repository Size, and File Count Limits

- Clone timeout enforced with context deadlines in `cmd/nodeplugin/main.go`.
- Size and file count measured and enforced in `pkg/materializer/materializer.go` and `pkg/materializer/fslimits.go`.

### Node-local Internal Cache

- Cache keying, directory management, and LRU plus max-age eviction in `pkg/cache/cache.go`.

### Mounted Metadata

- `.gitcontent/*` metadata files written via `pkg/metadata/write.go`.
- Materialized content is copied into a content-only mount tree in `pkg/materializer/materializer.go`; internal `.git` directories are skipped and remain outside the workload-visible tree.

### Helm CSI Deployment

- The Helm chart creates a `CSIDriver` object with `podInfoOnMount=true`, `attachRequired=false`, and `volumeLifecycleModes: [Ephemeral]`.
- The node plugin DaemonSet includes `csi-node-driver-registrar` and mounts the kubelet plugin, plugin registry, pod, policy, and cache paths needed for kubelet registration and node publish operations.
- The node plugin no longer performs Linux bind mounts and does not require `privileged: true`, mount propagation, `CAP_SYS_ADMIN`, or elevated workload permissions. The deployable chart default runs the node plugin process as UID `0` with all capabilities dropped so it can write kubelet-owned target paths, and uses GID `10001` for cache writes. Git subprocess UID/GID switching is implemented behind `GITCONTENT_GIT_RUN_AS_UID` and `GITCONTENT_GIT_RUN_AS_GID`, but is disabled in the default chart because it requires `CAP_SETUID`/`CAP_SETGID`; a separate non-root materializer helper is the preferred next hardening step.

### Kubernetes Events Equivalent + Structured Reasons

- Event reason constants and payload model in `pkg/observability/events.go`.
- Denial and error reason codes are carried by policy results and mount responses.

### Prometheus Metrics

- RFC metric families implemented in `pkg/observability/metrics.go`.
- Metrics exposed at `/metrics` in `cmd/nodeplugin/main.go`.

### Admission Webhook

- Pod validation webhook implemented in `cmd/admission-webhook/main.go`.
- Rejects invalid/unapproved volumes before scheduling.

## Security Controls Implemented

- Read-only mount requirement in admission and request shape.
- Git hooks disabled by explicit Git command configuration in `pkg/materializer/materializer.go`.
- Git file protocol disabled for materializer Git commands in `pkg/materializer/materializer.go`.
- Git subprocesses can drop to a configured UID/GID through `GITCONTENT_GIT_RUN_AS_UID` and `GITCONTENT_GIT_RUN_AS_GID` when the runtime grants `CAP_SETUID`/`CAP_SETGID`. The Helm chart keeps this disabled by default to avoid adding capabilities to the node plugin.
- Local filesystem and `file://` repositories rejected in `pkg/volume/attributes.go`.
- `.git` directories excluded from mounted content in `pkg/materializer/materializer.go`.
- Git LFS disabled by default and policy-gated.
- Submodules disabled by default and policy-gated.
- No writable repository exposure to workload path in mount pipeline.

## Security Rationale from Legacy `gitRepo` CVEs

- CVE-2024-10220 showed that repository-controlled Git hooks in the in-tree
  `gitRepo` volume path could lead to command execution beyond the container
  boundary while kubelet performed Git operations. This implementation disables
  hooks through Git command-line configuration and exposes a separate
  materialized content tree instead of the internal Git repository.
- CVE-2025-1767 showed that local repository paths in `gitRepo` could disclose
  other repositories already present on the same node. This implementation
  rejects local filesystem paths and `file://` repository URLs before policy
  evaluation or clone.
- Kubernetes 1.36 and later no longer include the in-tree `gitRepo` volume
  driver. This project is therefore a CSI-based replacement pattern with
  explicit policy, provenance metadata, safer content boundaries, and central
  operational controls, not a drop-in recreation of the removed volume type.

## Example Config

- Policy examples aligned to RFC in `examples/policies.yaml`.

## Test Coverage

- Attribute parser validation tests in `pkg/volume/attributes_test.go`.
- Policy enforcement tests in `pkg/policy/evaluator_test.go`.
- Opt-in kind E2E tests in `test/e2e` build local node plugin and Git fixture
  images, create a temporary kind cluster, install the Helm chart, and validate
  branch, tag, and commit materialization through real inline CSI volumes
  mounted into workload Pods. The test also checks mounted content and
  `.gitcontent/resolved-revision` metadata from inside the workload container.
