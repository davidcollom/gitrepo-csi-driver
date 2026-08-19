# Git Content CSI Driver

The project is motivated by the removal of Kubernetes' deprecated in-tree
`gitRepo` volume driver in Kubernetes 1.36 and later. The intent is not to
recreate `gitRepo` as a compatibility shim, but to provide a stronger and more
flexible CSI-based approach with policy enforcement, central credentials,
auditable revision metadata, and safer materialization boundaries.

## Components

- `cmd/nodeplugin`: CSI node service plus debug HTTP mount pipeline (policy, materialization, metadata, metrics, events)
- `cmd/admission-webhook`: admission validation for Pods requesting git content CSI volumes
- `pkg/policy`: `GitContentPolicy` and `GitCredentialProfile` modeling and enforcement
- `pkg/volume`: CSI-style volume attributes parsing and validation
- `pkg/materializer`: constrained Git clone/checkout flow with no-hook execution and hard limits
- `pkg/cache`: node-local cache index with max size, max age, and LRU eviction
- `pkg/metadata`: mounted `.gitcontent/*` metadata files
- `pkg/observability`: metrics and event reason models

## Security Defaults

- Read-only mounts only
- Git hooks disabled
- Local filesystem repository references rejected
- `.git` internals excluded from mounted content
- Submodules disabled by default
- Git LFS disabled by default
- Pinned revision support with policy enforcement
- Host/repository allowlist enforcement
- Clone timeout, size, and file count limits

## Status

This is an MVP foundation and is intentionally focused on safety and policy enforcement.

## Run

1. Start the node plugin service:

```bash
go run ./cmd/nodeplugin
```

2. Start the admission webhook service:

```bash
go run ./cmd/admission-webhook
```

3. Use `POST /mount` on the node plugin with a JSON payload containing namespace, targetPath, and volumeAttributes for local/debug materialization.

The same binary also serves the CSI node service on `CSI_ENDPOINT`, defaulting
to `unix:///csi/csi.sock`.

## Lint and Tests

Run lint locally:

```bash
golangci-lint run --config .golangci-lint.yml
```

Run tests:

```bash
go test ./...
```

Run opt-in kind E2E tests:

```bash
RUN_E2E=1 go test -tags=e2e ./test/e2e -v
```

The E2E suite builds local test images, creates a uniquely named kind cluster,
installs the Helm chart with the CSI node driver registrar, deploys an
in-cluster Git fixture, and validates branch, tag, and commit materialization
through real inline CSI volumes mounted into workload Pods. It deletes the
test-created cluster on completion unless `E2E_KEEP_CLUSTER=1` is set.

## Helm Deployment

Install the chart:

```bash
helm upgrade --install gitrepo-csi-driver ./helm/gitrepo-csi-driver -n git-content-system --create-namespace
```

Override the policy file through values as needed.

## Example Workloads

See `examples/deploy` for:

- `php-dynamic-site-tag.yaml` for immutable tag-based content
- `php-dynamic-site-branch.yaml` for mutable branch tracking
- `rollout-restart-cronjob.yaml` for scheduled refresh via rollout restarts

Branch and tag references are resolved at Pod start. For mutable references, restart workloads to pull updated content.

## Release Automation

- GoReleaser configuration: `.goreleaser.yml`
- CI workflow: `.github/workflows/ci.yml`
- Release workflow: `.github/workflows/release.yml`

Release builds publish multi-arch images with `dockers_v2` and sign release checksum artifacts using `cosign`.

## RFC Coverage

Detailed requirement mapping is in `docs/implementation.md`.
