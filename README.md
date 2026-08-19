# Git Content CSI Driver

`gitrepo-csi-driver` is a Kubernetes CSI driver for mounting Git repository
content into Pods as read-only ephemeral volumes.

The project exists because the in-tree Kubernetes `gitRepo` volume driver is
gone in Kubernetes 1.36 and later. That old driver was convenient, but it put
Git clone behaviour inside kubelet and carried security problems around hook
execution and access to local repositories on a node.

This driver takes a different shape. Workloads ask for approved Git content
through an inline CSI volume. The node plugin evaluates policy, clones the
repository, checks out the requested branch, tag, or commit, writes provenance
metadata, and publishes only the materialized content into the Pod.

It is not a drop-in replacement for `gitRepo`. It is a safer replacement
pattern for platform teams that want Git-backed runtime content without asking
every application team to maintain its own init container, credentials, retry
logic, cache handling, and policy checks.

## What it does

- Serves a real CSI node service over gRPC.
- Supports inline ephemeral CSI volumes.
- Installs with a Helm chart and the CSI node-driver registrar.
- Clones remote `http`, `https`, `ssh`, and scp-like Git repositories.
- Supports branch, tag, and pinned commit refs.
- Enforces repository, host, revision, path, depth, submodule, and LFS policy.
- Publishes read-only content to the workload.
- Excludes `.git` internals from the workload-visible filesystem.
- Writes `.gitcontent` metadata, including the resolved commit.
- Exposes Prometheus metrics.
- Includes an admission webhook for early validation.

## Why not just use an init container?

Init containers are still a valid answer for some workloads. They are also easy
to copy-paste badly.

Once many teams need Git content at runtime, the same problems tend to repeat:
credentials spread across namespaces, branch and tag usage is inconsistent,
clone failures are hard to see, cache behaviour depends on each workload, and
security teams have no single place to enforce what can be mounted.

This driver moves those concerns into a platform-owned component. Application
Pods describe the content they need. The platform decides which repositories,
refs, paths, and features are allowed.

## Security model

The driver is designed around lessons from the removed `gitRepo` volume plugin:

- Git hooks are disabled with Git command-line configuration.
- Local filesystem paths and `file://` repository URLs are rejected.
- The internal Git repository is not mounted into workloads.
- The workload receives a separate content tree plus `.gitcontent` metadata.
- Submodules and Git LFS are disabled unless policy allows them.
- Workloads do not need privileged mode, hostPath mounts, runtime sockets, or
  elevated Linux capabilities.

The node plugin does not use privileged mode, mount propagation, or
`CAP_SYS_ADMIN`. The current Helm default runs the node plugin as UID `0` with
all Linux capabilities dropped, because kubelet-owned CSI target paths are
created dynamically on the node. Git subprocess UID/GID dropping is implemented
but disabled by default because it requires `CAP_SETUID` and `CAP_SETGID`; the
preferred next hardening step is a separate non-root materializer helper.

## Status

This is an MVP. It already installs into kind with Helm and passes an E2E suite
that exercises real CSI gRPC volume publishing for branch, tag, and commit refs.

The design intent and implementation notes live in:

- `docs/rfc.md`
- `docs/implementation.md`

Those documents are the source of truth for design decisions in this project.

## Install with Helm

```bash
helm upgrade --install gitrepo-csi-driver ./helm/gitrepo-csi-driver \
  --namespace gitrepo-csi-system \
  --create-namespace
```

The chart installs:

- a `CSIDriver` object
- a node plugin `DaemonSet`
- the CSI node-driver registrar
- the admission webhook, when enabled
- the policy `ConfigMap`, unless an existing one is supplied

Override `policy.content` or set `policy.existingConfigMap` to provide your own
policy.

## Example inline CSI volume

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: git-content-example
spec:
  containers:
    - name: app
      image: nginx:alpine
      volumeMounts:
        - name: content
          mountPath: /usr/share/nginx/html
          readOnly: true
  volumes:
    - name: content
      csi:
        driver: gitcontent.csi.example.io
        readOnly: true
        volumeAttributes:
          repo: https://github.com/example/site-content.git
          revision: refs/tags/v1.0.0
          revisionKind: tag
          path: public
          policy: default
```

Branch and tag refs are resolved when the Pod starts. For mutable refs, restart
the workload to fetch newer content.

## Local development

Run the unit tests:

```bash
go test ./...
```

Run vet:

```bash
go vet ./...
```

Lint the Helm chart:

```bash
helm lint helm/gitrepo-csi-driver
```

Run the kind E2E suite:

```bash
RUN_E2E=1 go test -tags=e2e ./test/e2e -v -count=1
```

The E2E test builds local images, creates a temporary kind cluster, installs the
Helm chart, deploys an in-cluster Git fixture, and verifies branch, tag, and
commit materialization from inside workload Pods.

To test a specific Kubernetes version through kind:

```bash
KIND_NODE_IMAGE=kindest/node:v1.36.1 \
RUN_E2E=1 go test -tags=e2e ./test/e2e -v -count=1
```

Set `E2E_KEEP_CLUSTER=1` if you want the test cluster left behind for
debugging.

## CI and releases

GitHub Actions runs normal CI for pushes and pull requests.

The E2E workflow can be run manually, runs weekly on a schedule, and is reused
as a pre-release gate. It tests a kind matrix across the latest published kind
node images for Kubernetes 1.36, 1.35, 1.34, and 1.33.

Releases are manual. For the first release, run the workflow with
`release_type=initial` and an initial version such as `v0.1.0` after CI and E2E
have passed on `main`. For later releases, the workflow calculates the next
version with `svu`, creates and pushes the Git tag that GoReleaser expects, and
then runs GoReleaser from that tag. Release builds publish multi-arch images to
GHCR and sign checksum artifacts with `cosign`.

The published container images are:

- `ghcr.io/davidcollom/gitrepo-csi-nodeplugin`
- `ghcr.io/davidcollom/gitrepo-csi-admission-webhook`

## Repository layout

- `cmd/nodeplugin`: CSI node service and debug HTTP materialization endpoint.
- `cmd/admission-webhook`: admission validation for Pods using Git content CSI
  volumes.
- `pkg/volume`: volume attribute parsing and validation.
- `pkg/policy`: policy models and enforcement.
- `pkg/materializer`: constrained Git clone, checkout, copy, and limit checks.
- `pkg/cache`: node-local cache management.
- `pkg/metadata`: mounted `.gitcontent` metadata.
- `pkg/observability`: metrics and event reason models.
- `helm/gitrepo-csi-driver`: Helm chart.
- `test/e2e`: kind-based E2E tests.

## Project scope

The driver provides read-only Git content. It does not provide writable mounts,
continuous sync, Git push/merge/rebase workflows, or a way to bypass normal
artifact release processes.

For immutable application artifacts, an image or OCI artifact may still be the
better choice. This driver is for cases where Git repo, path, and revision
semantics matter at Pod start.
