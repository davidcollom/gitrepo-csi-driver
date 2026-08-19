# Git Content CSI Driver

## Components

- `cmd/nodeplugin`: mount request processing pipeline (policy, materialization, metadata, metrics, events)
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

3. Use `POST /mount` on the node plugin with a JSON payload containing namespace, targetPath, and volumeAttributes.

## Lint and Tests

Run lint locally:

```bash
golangci-lint run --config .golangci-lint.yml
```

Run tests:

```bash
go test ./...
```

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
