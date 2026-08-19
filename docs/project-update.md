# Project update: public MVP

Published: 2026-08-19

`gitrepo-csi-driver` is now public. It is a Kubernetes CSI driver for mounting approved Git repository content into Pods as read-only ephemeral volumes.

The project provides a platform-managed replacement pattern for teams affected by the removal of the in-tree Kubernetes `gitRepo` volume driver in Kubernetes 1.36. It is not a drop-in recreation of `gitRepo`. The goal is to move repeated Git clone concerns into a shared platform component with policy, provenance and operational visibility.

## Current status

This is an MVP. The driver installs into kind with Helm and has E2E coverage for real CSI gRPC volume publishing using branch, tag and commit refs.

Current capabilities include:

- inline ephemeral CSI volumes;
- Helm installation;
- remote `http`, `https`, `ssh` and scp-like Git repository URLs;
- branch, tag and pinned commit refs;
- repository and host allowlists;
- revision, path, depth, submodule and LFS policy controls;
- read-only content publishing;
- `.gitcontent` metadata with the resolved commit;
- Prometheus metrics;
- an admission webhook for early validation;
- kind-based E2E coverage across Kubernetes 1.33, 1.34, 1.35 and 1.36.

## Intended use cases

The driver is intended for Git-backed runtime content where repository, path and revision semantics matter at Pod start.

Good candidate workloads include:

- static site content;
- documentation and runbook content;
- policy bundles;
- templates and scaffolding assets;
- generated reports;
- dashboards;
- shared read-only scripts or operational helper content.

It is not intended to replace application image builds, OCI artefacts, release signing, SBOM generation, vulnerability scanning or other normal software release controls.

## Security model

The MVP is designed around the known failure modes of the removed `gitRepo` volume plugin.

Implemented controls include:

- Git hooks disabled through Git command-line configuration;
- local filesystem paths and `file://` repositories rejected;
- `.git` internals excluded from the workload-visible filesystem;
- separate materialised content trees for workloads;
- read-only volume publishing;
- submodules and Git LFS disabled unless policy allows them;
- no workload requirement for privileged mode, hostPath mounts, runtime sockets or elevated Linux capabilities.

The node plugin currently runs as UID `0` by default with all Linux capabilities dropped, because kubelet-owned CSI target paths are created dynamically on the node. A separate non-root materialiser helper is the preferred next hardening step.

## How to try it

Install the chart from a checkout of this repository:

```bash
helm upgrade --install gitrepo-csi-driver ./helm/gitrepo-csi-driver \
  --namespace gitrepo-csi-system \
  --create-namespace
```

Then configure policy with `policy.content` or `policy.existingConfigMap` and create a Pod with an inline CSI volume using the `gitcontent.csi.example.io` driver name.

See the main [README](../README.md), [RFC](rfc.md), [implementation coverage](implementation.md) and [deployment examples](../examples/deploy/README.md) for the current details.

## Next work

Likely next areas include:

- tightening the non-root materialisation path;
- expanding policy examples for common production and development patterns;
- improving documentation for credentials and private repositories;
- publishing the first tagged release through the existing release workflow;
- gathering feedback from platform teams that still need controlled Git-backed content in Kubernetes.
