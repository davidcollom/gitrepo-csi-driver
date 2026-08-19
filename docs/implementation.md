# RFC Implementation Coverage

This document maps the RFC MVP scope to concrete implementation in this repository.

## Implemented MVP Features

### CSI-style Ephemeral Read-only Volume Request Flow

- Implemented as a node plugin HTTP mount pipeline in `cmd/nodeplugin/main.go`.
- Enforces read-only usage semantics and mount-time policy checks before materialization.

### HTTPS and SSH Repository URL Support

- URL handling supports `https://`, `ssh://`, and scp-like host extraction in `pkg/policy/evaluator.go`.
- Materialization executes standard Git clone and checkout operations in `pkg/materializer/materializer.go`.

### Pinned Commit, Tag, and Branch Policy Controls

- Revision controls implemented in `pkg/policy/evaluator.go`.
- `requirePinnedCommit`, `allowBranches`, `allowTags`, and regex pattern checks are enforced.

### Shallow Clone and Optional Path Selection

- Depth defaults and max depth constraints are policy-driven in `pkg/policy/load.go` and enforced in `pkg/policy/evaluator.go`.
- Clone depth is applied during materialization and optional `path` selection is supported in `pkg/materializer/materializer.go`.

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
- Git hooks disabled by runtime environment in `pkg/materializer/materializer.go`.
- Git LFS disabled by default and policy-gated.
- Submodules disabled by default and policy-gated.
- No writable repository exposure to workload path in mount pipeline.

## Example Config

- Policy examples aligned to RFC in `examples/policies.yaml`.

## Test Coverage

- Attribute parser validation tests in `pkg/volume/attributes_test.go`.
- Policy enforcement tests in `pkg/policy/evaluator_test.go`.
