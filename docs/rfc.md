# RFC: Git Content CSI Driver for Kubernetes

## Status

Draft

## Summary

This RFC proposes a Kubernetes CSI driver that mounts Git repository content into Pods as **read-only ephemeral volumes**. The driver is intended as a secure, platform-managed replacement pattern for workloads that previously relied on the removed in-tree `gitRepo` volume plugin, while avoiding the same security and operational pitfalls.

The driver should not be positioned as a direct `gitRepo` compatibility layer. Instead, it should provide a controlled mechanism for mounting Git-hosted content such as static site assets, documentation, policy bundles, templates, shared read-only library code, scripts, dashboards, and other non-mutating content.

The primary goal is to allow application teams to consume approved Git content without each workload needing to manage Git credentials, clone logic, SSH keys, tokens, retry behaviour, caching, or repository policy enforcement.

## Problem Statement

Kubernetes removed the legacy `gitRepo` volume plugin due to security and maintainability concerns. The most common migration paths are init containers, sidecars such as `git-sync`, or packaging content as OCI artifacts.

Those approaches are valid, but they leave a gap for platform teams that want to provide a standard, governed, observable, and credential-managed way for workloads to consume read-only Git content.

Common challenges with per-workload Git cloning include:

* Every team has to implement and maintain clone logic.
* Git credentials are distributed across namespaces and workloads.
* Repository access policy is inconsistent.
* Startup performance varies depending on network and Git host availability.
* Caching is duplicated or absent.
* Submodule behaviour is difficult to govern safely.
* Branch, tag, and commit pinning policies are not centrally enforced.
* Failures are hidden inside arbitrary init containers or application startup logic.
* Security teams have limited visibility into what Git content is being mounted.

## Goals

* Provide read-only Git content mounts to Pods through CSI ephemeral volumes.
* Allow platform teams to manage Git credentials centrally.
* Allow engineers to consume approved repositories without directly handling credentials.
* Enforce cluster-level and namespace-level Git content policies.
* Support pinned revisions, branches, and tags, with policy controls.
* Support optional submodule cloning, disabled by default.
* Provide safe defaults for repository size, clone depth, timeouts, and allowed hosts.
* Provide Prometheus metrics and Kubernetes Events for operational visibility.
* Avoid hostPath mounts, privileged workload access, runtime sockets, or node filesystem exposure to application containers.
* Provide a safer and more auditable alternative to ad-hoc init container cloning.

## Non-Goals

* Recreate the legacy `gitRepo` volume plugin as a drop-in compatibility shim.
* Provide writable Git mounts.
* Commit, push, merge, rebase, or mutate Git repositories.
* Continuously sync content after Pod startup in the MVP.
* Replace `git-sync` for live synchronisation use cases.
* Replace OCI image volumes for immutable released artifacts.
* Execute arbitrary Git hooks.
* Allow arbitrary repositories without policy approval.
* Mount host Git cache directories directly into workloads.

## Use Cases

The driver is most compelling where teams need repository-backed content at runtime, but the platform team wants to avoid every workload owning its own Git credentials, clone commands, retry logic, caching, and security controls.

The underlying workload pattern has not disappeared with the removal of the in-tree `gitRepo` volume plugin. Kubernetes still recommends patterns such as init containers or external synchronisation tooling for teams that need Git content inside Pods. This creates an opportunity for a safer, platform-owned implementation that focuses on governance, observability, credentials, and repeatability rather than ad-hoc cloning.

The use cases below are grouped by the kind of value the driver provides.

### Use Case Summary

| Category                        | Example Use Cases                                          | Why It Matters                                                          |
| ------------------------------- | ---------------------------------------------------------- | ----------------------------------------------------------------------- |
| Static content delivery         | Static sites, documentation, app assets, generated reports | Read-only Git content maps naturally to filesystem mounts               |
| Platform-managed shared content | Shared scripts, templates, config bundles, policy bundles  | Reduces duplicated clone logic and credential sprawl                    |
| Governed runtime dependencies   | Rules, plugins, schemas, small knowledge packs             | Allows platform teams to control what runtime content workloads consume |
| Operational acceleration        | Jobs, demos, workshops, test fixtures                      | Makes short-lived workloads easier to run consistently                  |
| Controlled composition          | Submodule-based docs, themes, policy baselines             | Enables useful Git composition while enforcing allowlists and limits    |

### Static Sites

A workload may serve a static site from a Git repository without baking the content into the application image.

Example content:

* Marketing sites
* Internal documentation
* Runbooks
* MkDocs, Hugo, Jekyll, or similar generated output
* Static dashboards
* Generated HTML reports
* Demo portals
* Status-style pages

Why CSI helps:

* Application teams do not need to own clone commands or Git credentials.
* Platform teams can enforce allowed repositories, revisions, paths, and submodule usage.
* Node-level or driver-level caching can reduce repeated clone overhead.
* The mounted content can be pinned to a known commit for repeatability.

### Documentation-as-Runtime-Content

Documentation is naturally Git-native and is often consumed as files by static site generators, internal portals, or lightweight web servers.

Example content:

* Internal knowledge bases
* On-call runbooks
* Troubleshooting guides
* Product documentation
* Backstage TechDocs-style content
* Markdown rendered by an internal service
* Shared documentation partials
* Versioned release notes

Why CSI helps:

* Documentation can be updated through normal Git review workflows.
* Static documentation services can consume approved content without embedding Git credentials.
* Optional submodule support can allow shared themes, snippets, and partials where explicitly approved.

### Static Application Assets

Some applications need read-only content that changes more often than the application runtime image.

Example content:

* Email templates
* PDF templates
* HTML snippets
* Theme files
* Branding assets
* Icons
* Localisation files
* Markdown content
* JSON schema files
* Generated static assets

Why CSI helps:

* Teams can separate runtime images from content managed in Git.
* Platform teams can require pinned revisions for production.
* Content updates can follow Git review and approval flows.

Caution:

* For immutable release artifacts, OCI image volumes may be a better fit.
* This driver is most useful when Git repo, path, and revision semantics are important.

### Shared Read-Only Scripts and Automation Libraries

Platform teams commonly maintain shared helper scripts or automation libraries used by Jobs, CronJobs, migration jobs, operational tooling, or internal automation.

Example content:

* Shared shell scripts
* Python utility modules
* Database migration helpers
* Report generation utilities
* Backup and restore scripts
* Operational runbooks as executable content
* Common CI/CD helper scripts used inside Kubernetes Jobs
* Remediation scripts

Why CSI helps:

* Teams can mount approved helper content instead of baking it into every image.
* Credentials remain platform-managed.
* The driver can enforce revision pinning, repository allowlists, and path restrictions.
* Jobs can be simpler and more repeatable.

Caution:

* Mounted scripts may be executed by the workload.
* Production usage should require pinned commits and tight repository allowlists.
* This should not become a way to bypass image build, signing, SBOM, or vulnerability scanning processes.

### Platform-Managed Configuration Bundles

Some configuration is better managed as versioned files than environment variables or Kubernetes ConfigMaps, particularly when it is large, structured, or shared across workloads.

Example content:

* App configuration fragments
* Routing tables
* Feature flag seed files
* Tenant mappings
* Region mappings
* NGINX snippets
* Envoy configuration fragments
* Non-secret application config
* Rules consumed by internal services

Why CSI helps:

* Configuration can be centrally reviewed and versioned in Git.
* Platform teams can allow application teams to consume approved repositories without exposing credentials.
* The driver can enforce repo/path/revision policy at admission and mount time.

Caution:

* Secrets should not be distributed through this driver.
* Secret material should remain in dedicated secret-management systems.

### Policy Bundles

Security, compliance, and platform teams often maintain policy content in Git.

Example content:

* OPA/Rego bundles
* Conftest policies
* Kyverno policy fragments
* Gatekeeper templates
* Security baseline definitions
* Internal compliance rules
* Custom scanner rules
* Admission controller rules
* Exception allowlists

Why CSI helps:

* Policy content is read-only and naturally versioned.
* Consumers can mount an approved policy revision without Git credentials.
* Security teams can enforce commit pinning and repository allowlists.
* Mounted metadata can record the exact policy revision in use.

### Shared Templates and Scaffolding

Internal developer platforms and automation services often need templates that are maintained in Git.

Example content:

* Helm values templates
* Kustomize bases
* Jsonnet libraries
* Cookiecutter templates
* Scaffolding templates
* Terraform module examples
* Kubernetes manifest templates
* Workflow templates
* Pull request templates

Why CSI helps:

* Templates can be managed centrally and consumed by multiple services.
* Consumers do not need direct Git credentials.
* Sub-path mounting can expose only the required template directory.
* Pinned revisions make generated output reproducible.

### Runtime Rule, Plugin, and Extension Directories

Some tools load plugins, rules, definitions, or extensions from the filesystem at startup.

Example content:

* Static analysis rules
* Linter plugins
* Parser definitions
* Custom validators
* Internal CLI plugins
* Workflow engine plugins
* Lightweight extension packs
* Scanner rules

Why CSI helps:

* Platform teams can govern which runtime extensions are allowed.
* Repository, host, path, and revision policy can be enforced centrally.
* Workloads can consume approved runtime dependencies without embedding credentials.

Caution:

* Runtime plugins or rules may execute code or materially change workload behaviour.
* These use cases should require pinned revisions, strict allowlists, and strong auditability.

### AI and RAG Knowledge Packs

Text-heavy knowledge packs are often easier for humans to maintain in Git than in object storage or container images.

Example content:

* Markdown knowledge bases
* Product documentation
* Support articles
* Prompt templates
* Evaluation datasets
* Small reference datasets
* Agent tool definitions
* Runbook corpora
* FAQ content

Why CSI helps:

* Git provides review, history, and rollback for human-maintained knowledge.
* The driver can mount a specific revision into retrieval, indexing, or agent workloads.
* Platform teams can restrict which knowledge repos can be consumed by which namespaces.

Caution:

* Large embeddings, model files, or large datasets are better suited to object storage or OCI artifacts.

### Test Fixtures and Golden Files

Ephemeral environments and integration test jobs often need known fixture data.

Example content:

* Integration test fixtures
* Golden files
* Contract test definitions
* OpenAPI examples
* Mock payloads
* Synthetic datasets
* Chaos experiment definitions
* Example Kubernetes manifests

Why CSI helps:

* Test jobs can mount a pinned fixture repository without rebuilding images.
* Fixtures can be shared across teams while remaining centrally governed.
* Golden-file changes can be reviewed through normal Git workflows.

### Customer-Specific Content Overlays

Multi-tenant platforms may need to serve or process customer-specific content safely.

Example content:

* Customer-specific branding
* Static customer documentation
* Tenant-specific rules
* Per-customer templates
* Configuration overlays
* Generated customer assets

Why CSI helps:

* Namespace or tenant-specific policies can restrict which repositories are accessible.
* Credentials remain platform-managed.
* Teams can mount only the approved path or revision for a tenant.

### Education, Workshops, and Demo Environments

Short-lived namespaces and demo environments often need example files, manifests, or training content.

Example content:

* Training lab content
* Workshop exercises
* Demo manifests
* Tutorial files
* Sample applications
* Hands-on scripts
* Temporary sandbox content

Why CSI helps:

* Demo environments can be created without building custom images for every workshop.
* Content can be pinned to a known revision for reproducibility.
* Platform teams can allow broad read-only access to approved training repositories.

### Small Static Datasets and Reference Data

Small, human-reviewable reference datasets are often suitable for Git.

Example content:

* Geo mappings
* Currency metadata
* Country and region mappings
* Validation datasets
* Lookup tables
* Compliance reference data
* Static taxonomies

Why CSI helps:

* Git gives review, history, approval, and rollback.
* Workloads can mount the dataset directly from an approved repository and revision.

Caution:

* Large datasets should use object storage, data platforms, or OCI artifacts instead.

### GitOps-Adjacent Operational Content

This driver is not a replacement for GitOps controllers such as Argo CD or Flux. Those tools reconcile Kubernetes resources into the cluster. This driver materialises Git content as a filesystem dependency inside a workload.

Example content:

* A controller needs rule files from Git
* An operator needs templates from Git
* A scanner needs allowlists from Git
* A remediation job needs scripts from Git
* A compliance job needs benchmark definitions from Git
* A controller needs generated manifests as input data rather than applied resources

Why CSI helps:

* It fills a gap between GitOps reconciliation and runtime file consumption.
* Operators and jobs can consume approved content without implementing Git clone logic.

### Controlled Submodule-Based Content Composition

Submodules are useful when one repository composes content from other repositories, but they significantly expand the trust boundary.

Example content:

* Static site repo pulling a shared theme repo
* Documentation repo pulling shared partials
* Policy repo pulling common baseline rules
* Training repo pulling shared workshop assets
* App template repo pulling shared scaffolding modules
* Customer content repo pulling shared brand assets

Why CSI helps:

* The driver can enable submodules only where explicitly allowed.
* Submodule repositories can be independently allowlisted.
* Recursive submodules can remain disabled by default.
* Submodule depth, size, timeout, and file count limits can be enforced.
* Submodule credentials can use approved credential profiles only.

This creates a safer alternative to arbitrary init containers running recursive Git clones with no central policy.

### Highest-Value Initial Use Cases

The first validation conversations should focus on the use cases most likely to justify the investment:

1. Static sites and documentation.
2. Policy bundles.
3. Shared scripts and templates for Jobs.
4. Platform-managed configuration bundles.
5. Controlled submodule-based site or documentation composition.
6. Test fixtures and golden files.
7. AI and RAG knowledge packs.
8. Runtime rules and plugins.
9. Customer-specific content overlays.
10. Workshop and demo content.

## Recommended Alternatives

This CSI driver should be documented alongside other valid patterns.

| Pattern                | Recommended When                                                                                 |
| ---------------------- | ------------------------------------------------------------------------------------------------ |
| Init container clone   | Simple one-off cloning owned by the application team                                             |
| `git-sync` sidecar     | Content must be continuously synchronised while the Pod is running                               |
| OCI image volume       | Content is immutable, released, signed, and distributed through an OCI registry                  |
| Git Content CSI Driver | Content should be mounted read-only with central credentials, policy, caching, and observability |

## Design Principles

### Read-Only by Design

All mounted content must be read-only from the workload perspective.

The driver must not expose writable repository directories to application containers. Any clone, fetch, checkout, or cache operation must happen outside the application container and be mounted as immutable content.

### Platform-Controlled Credentials

Application teams should not need to define Git credentials in each workload.

Credentials should be referenced through platform-owned configuration. Namespace-level delegation may be supported, but workloads should only request approved content.

### Safe Defaults

The driver should default to conservative behaviour:

* Submodules disabled.
* Git LFS disabled.
* Shallow clone enabled.
* Read-only mounts only.
* Hooks disabled.
* Repository host allowlists required.
* Clone timeout enforced.
* Maximum repository size enforced.
* Maximum file count enforced.
* Branch usage optionally restricted.
* Commit SHA pinning recommended and optionally required.

### Policy-First Operation

Every mount request should be evaluated against one or more `GitContentPolicy` resources.

The policy should decide whether a repository, host, path, revision, submodule setting, LFS setting, and credential source are allowed.

### No Git Hook Execution

The driver must prevent Git hooks from executing.

The driver should never execute repository-provided scripts as part of clone, checkout, submodule, or LFS operations.

### No Privileged Workload Access

Workloads using the volume must not require:

* `privileged: true`
* hostPath mounts
* container runtime sockets
* node filesystem access
* elevated Linux capabilities
* direct access to Git credentials

### Observable and Auditable

The driver should make Git content mounts visible through:

* Kubernetes Events
* Prometheus metrics
* Structured logs
* Mounted revision metadata
* Optional audit records

## Architecture

```text
+--------------------+       +-------------------------+
| Application Pod    |       | Git Content CSI Driver  |
|                    |       |                         |
| /content readonly  |<------| Node Plugin             |
|                    |       |                         |
+--------------------+       +-----------+-------------+
                                        |
                                        v
                              +-------------------------+
                              | Git Materialiser        |
                              |                         |
                              | clone/fetch/checkout    |
                              | no hooks                |
                              | constrained runtime     |
                              +-----------+-------------+
                                          |
                          +---------------+---------------+
                          |                               |
                          v                               v
                +-------------------+           +-------------------+
                | Node-local cache  |           | Git provider       |
                | internal only     |           | GitHub/GitLab/etc  |
                +-------------------+           +-------------------+
```

## Core Components

### CSI Node Plugin

The CSI node plugin handles ephemeral volume mount requests from kubelet.

Responsibilities:

* Parse CSI volume attributes.
* Resolve applicable policy.
* Request credentials from the configured credential provider.
* Materialise the requested Git content.
* Mount content into the Pod as read-only.
* Emit metrics and events.

### Git Materialiser

The materialiser performs Git operations in a constrained environment.

Responsibilities:

* Clone or fetch repositories.
* Checkout the requested revision.
* Optionally initialise submodules if policy allows.
* Enforce clone depth.
* Enforce path filtering if supported.
* Enforce timeout, file count, and size limits.
* Produce immutable content for the workload mount.

The materialiser should run with:

* Non-root user.
* Read-only root filesystem where possible.
* No privilege escalation.
* Seccomp profile.
* AppArmor or SELinux profile where available.
* Minimal filesystem access.
* No access to workload containers.

### Policy Controller

The policy controller validates and reconciles `GitContentPolicy` resources.

Responsibilities:

* Validate allowed hosts.
* Validate credential references.
* Validate defaults and limits.
* Publish effective policy status.
* Optionally expose admission checks for Pods using the driver.

### Admission Webhook

An optional admission webhook should validate Pods that use the CSI driver before they are admitted.

Responsibilities:

* Reject disallowed repositories.
* Reject unpinned revisions where policy requires commit SHA pinning.
* Reject submodule usage where disabled.
* Reject Git LFS usage where disabled.
* Reject unsupported volume attributes.
* Reject repositories not matching namespace policy.

## Example Pod Usage

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: static-site
  namespace: web
spec:
  containers:
    - name: nginx
      image: nginx:1.27
      volumeMounts:
        - name: site-content
          mountPath: /usr/share/nginx/html
          readOnly: true
  volumes:
    - name: site-content
      csi:
        driver: gitcontent.csi.example.io
        readOnly: true
        volumeAttributes:
          repo: https://github.com/example/static-site.git
          revision: 8f3c2b7a9e4f3f6a9b2a4f1e6c9d8e1a2b3c4d5e
          path: public
          depth: "1"
          submodules: "false"
```

## Example Shared Library Usage

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: report-generator
  namespace: reporting
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: report-generator
          image: python:3.13-slim
          command:
            - python
            - /opt/shared-libs/reports/generate.py
          volumeMounts:
            - name: shared-libs
              mountPath: /opt/shared-libs
              readOnly: true
      volumes:
        - name: shared-libs
          csi:
            driver: gitcontent.csi.example.io
            readOnly: true
            volumeAttributes:
              repo: https://github.com/example/platform-shared-libs.git
              revision: v1.4.2
              path: python
              depth: "1"
              submodules: "false"
```

## Volume Attributes

| Attribute           | Required | Description                                                           |
| ------------------- | -------: | --------------------------------------------------------------------- |
| `repo`              |      Yes | Git repository URL                                                    |
| `revision`          |      Yes | Commit SHA, tag, or branch depending on policy                        |
| `path`              |       No | Optional sub-path within the repository to mount                      |
| `depth`             |       No | Shallow clone depth. Defaults from policy                             |
| `submodules`        |       No | Whether submodules should be initialised. Defaults to `false`         |
| `lfs`               |       No | Whether Git LFS should be enabled. Defaults to `false`                |
| `policy`            |       No | Optional policy name to request, if namespace policy allows selection |
| `credentialProfile` |       No | Optional platform-defined credential profile                          |

## Content Policy CRD

```yaml
apiVersion: gitcontent.example.io/v1alpha1
kind: GitContentPolicy
metadata:
  name: default
spec:
  selector:
    namespaces:
      - web
      - reporting

  allowedRepositories:
    - https://github.com/example/*
    - ssh://git@gitlab.internal.example.com/platform/*

  allowedHosts:
    - github.com
    - gitlab.internal.example.com

  revisions:
    requirePinnedCommit: true
    allowBranches: false
    allowTags: true
    allowedBranchPatterns: []
    allowedTagPatterns:
      - '^v[0-9]+\.[0-9]+\.[0-9]+$'

  clone:
    defaultDepth: 1
    maxDepth: 10
    timeout: 30s
    maxRepositorySize: 100Mi
    maxFileCount: 5000
    allowSparseCheckout: true

  submodules:
    enabled: false
    maxDepth: 1
    recursive: false
    allowedRepositories: []
    allowedHosts: []

  lfs:
    enabled: false
    maxObjectSize: 25Mi
    maxTotalSize: 100Mi

  credentials:
    defaultProfile: github-readonly
    allowedProfiles:
      - github-readonly
      - internal-gitlab-readonly
```

## Optional Submodule Support

Submodules are useful, but they expand the trust boundary because a repository can reference other repositories.

Submodule support should be available, but disabled by default.

When enabled, submodules should be governed independently from the parent repository.

Recommended controls:

* Submodules disabled by default.
* Recursive submodules disabled by default.
* Maximum submodule depth enforced.
* Submodule repositories must match an allowlist.
* Submodule hosts must match an allowlist.
* Submodule revisions should be pinned.
* Submodule clone timeout enforced.
* Submodule size and file count limits enforced.
* Submodule credentials must use approved credential profiles.

Example policy enabling constrained submodules:

```yaml
apiVersion: gitcontent.example.io/v1alpha1
kind: GitContentPolicy
metadata:
  name: static-sites-with-submodules
spec:
  selector:
    namespaces:
      - web

  allowedRepositories:
    - https://github.com/example/static-sites/*

  allowedHosts:
    - github.com

  revisions:
    requirePinnedCommit: true
    allowBranches: false
    allowTags: true

  submodules:
    enabled: true
    recursive: false
    maxDepth: 1
    allowedRepositories:
      - https://github.com/example/static-site-themes/*
      - https://github.com/example/shared-assets/*
    allowedHosts:
      - github.com
```

## Credential Management

The driver should support platform-managed credentials through credential profiles.

A credential profile should map to one or more Kubernetes Secrets, external secret references, or cloud-native identity mechanisms.

Application workloads should not receive raw Git credentials.

Example credential profile:

```yaml
apiVersion: gitcontent.example.io/v1alpha1
kind: GitCredentialProfile
metadata:
  name: github-readonly
  namespace: git-content-system
spec:
  type: githubApp
  secretRef:
    name: github-app-readonly
  allowedRepositories:
    - https://github.com/example/*
```

Alternative credential types may include:

* HTTPS token
* SSH deploy key
* GitHub App
* GitLab deploy token
* Cloud provider workload identity
* External Secrets Operator-managed Secret

## Revision Pinning

The safest production behaviour is to require commit SHA pinning.

Tags and branches are convenient, but they can move. Policies should allow platform teams to decide whether mutable references are permitted.

Recommended defaults:

| Environment | Recommended Policy                    |
| ----------- | ------------------------------------- |
| Production  | Require full commit SHA               |
| Staging     | Allow tags and commit SHAs            |
| Development | Allow branches, tags, and commit SHAs |

When a branch or tag is used, the driver should record the resolved commit SHA in events, logs, metrics, and mounted metadata.

## Mounted Metadata

The driver should optionally expose metadata files inside the mounted volume.

Example:

```text
.gitcontent/repo
.gitcontent/requested-revision
.gitcontent/resolved-revision
.gitcontent/mounted-at
.gitcontent/policy
.gitcontent/submodules-enabled
```

This helps application owners and support teams understand exactly what content was mounted.

The `.git` directory itself should not be mounted into the workload by default.

## Caching Strategy

The driver should support internal caching to reduce repeated clone overhead.

Cache keys should include:

* Repository URL
* Resolved commit SHA
* Path
* Submodule settings
* LFS settings
* Credential profile where relevant

The workload should not mount the mutable cache directly. Instead, the driver should materialise content from cache into an immutable read-only mount.

Cache controls:

* Maximum cache size per node.
* Maximum age.
* Least-recently-used eviction.
* Manual cache invalidation support.
* Metrics for hit and miss ratio.

## Failure Behaviour

If content cannot be materialised, the Pod should fail to start and the failure should be visible through Kubernetes Events.

Example failure reasons:

* Repository not allowed by policy.
* Git host not allowed by policy.
* Revision not allowed by policy.
* Submodules requested but disabled.
* Submodule repository not allowed.
* Clone timeout exceeded.
* Repository size limit exceeded.
* File count limit exceeded.
* Credential profile unavailable.
* Authentication failed.
* Revision not found.

## Kubernetes Events

Example successful event:

```text
Normal GitContentMounted Mounted Git content from https://github.com/example/static-site.git at revision 8f3c2b7 using policy default
```

Example denied event:

```text
Warning GitContentDenied Repository https://github.com/unknown/repo.git is not allowed by GitContentPolicy default
```

Example submodule denial:

```text
Warning GitContentDenied Submodules were requested but are disabled by GitContentPolicy default
```

## Metrics

Recommended Prometheus metrics:

```text
gitcontent_mount_requests_total{namespace,policy,result}
gitcontent_mount_duration_seconds{namespace,policy,result}
gitcontent_clone_duration_seconds{host,result}
gitcontent_cache_hits_total{host}
gitcontent_cache_misses_total{host}
gitcontent_policy_denials_total{namespace,policy,reason}
gitcontent_repository_size_bytes{host}
gitcontent_repository_files_total{host}
gitcontent_submodule_requests_total{namespace,policy,result}
gitcontent_credentials_errors_total{profile,reason}
```

## Security Considerations

### Git Hooks

Repository-provided hooks must never execute.

The driver should explicitly configure Git to avoid hook execution and avoid commands that invoke repository-controlled scripts.

### Submodules

Submodules must be treated as additional repositories and independently validated.

A parent repository being allowed does not automatically mean its submodules are allowed.

### Credentials

Workloads must not receive raw credentials.

Credentials should only be accessible to the CSI driver and materialiser. Credential scope should be read-only and restricted to the allowed repositories.

### Repository Size Abuse

Attackers or misconfigured users could reference very large repositories, many small files, or expensive submodule trees.

Policies must enforce hard limits.

### Mutable References

Branches and tags may move.

Production policies should require commit SHA pinning or record the resolved SHA for traceability.

### Network Egress

The driver should support egress restrictions through Kubernetes NetworkPolicy or equivalent CNI controls.

Only approved Git hosts should be reachable from the driver namespace.

### Host Filesystem

The driver must not expose internal host cache paths directly to workloads.

Workloads should receive only the materialised read-only content.

## Operational Considerations

### Availability

Git host availability can affect Pod startup. Caching can reduce this risk, but cache behaviour must be explicit.

Possible modes:

| Mode          | Behaviour                                                               |
| ------------- | ----------------------------------------------------------------------- |
| Strict        | Always verify/fetch the requested revision                              |
| Cached        | Use cache if the exact resolved revision exists                         |
| Offline cache | Allow cached content when Git host is unavailable, only for pinned SHAs |

### Startup Latency

Mounting Git content adds Pod startup latency.

The driver should expose mount duration metrics and optionally support pre-warming cache for known repositories.

### Debugging

Operators should be able to inspect:

* Requested repository
* Requested revision
* Resolved revision
* Effective policy
* Credential profile
* Denial reason
* Clone duration
* Cache hit/miss status

## MVP Scope

The initial implementation should include:

* CSI ephemeral read-only volumes.
* HTTPS repository cloning.
* SSH repository cloning if credentials are configured.
* Pinned commit SHA support.
* Tag support if policy allows.
* Branch support if policy allows.
* Shallow clone support.
* Optional path selection.
* Submodules disabled by default.
* Optional non-recursive submodule support behind policy.
* Repository and host allowlists.
* Platform-managed credential profiles.
* Clone timeout enforcement.
* Repository size limit.
* File count limit.
* Node-local internal cache.
* Kubernetes Events.
* Prometheus metrics.
* Admission webhook for policy enforcement.

## Post-MVP Scope

Potential future capabilities:

* Sparse checkout support.
* Git LFS support behind policy.
* Recursive submodule support behind stricter policy.
* Commit signature verification.
* Tag signature verification.
* GitHub App installation scoping.
* External Secrets Operator integration examples.
* Argo CD and Flux examples.
* Cache pre-warming controller.
* Namespace self-service policy delegation.
* VolumeSnapshot integration for debugging.
* `VolumeAttributesClass` support where appropriate.
* OCI artifact fallback or migration helpers.

## Example Helm Values

```yaml
controller:
  replicas: 2

nodePlugin:
  tolerations:
    - operator: Exists
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi

admissionWebhook:
  enabled: true

metrics:
  enabled: true
  serviceMonitor:
    enabled: true

cache:
  enabled: true
  maxSize: 10Gi
  maxAge: 24h

security:
  runAsNonRoot: true
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false

policy:
  defaultRequirePinnedCommit: true
  defaultAllowSubmodules: false
  defaultAllowLFS: false
```

## Open Questions

1. Should branch references be allowed at all in production namespaces?
2. Should the driver support continuous refresh, or should that remain explicitly out of scope in favour of `git-sync`?
3. Should the `.git` directory ever be mounted into workloads?
4. Should cache be per-node only, or should a central cache service be supported?
5. Should policies be cluster-scoped, namespace-scoped, or both?
6. Should credential profiles be selectable by workloads, or only assigned by policy?
7. Should submodule support require explicit approval per parent repository?
8. Should the driver support Git LFS in the first release?
9. Should mounted content include generated provenance metadata?
10. Should OCI artifact volumes be recommended as the preferred production path for immutable static content?

## Recommendation

This project is worth exploring if it is positioned as a secure, read-only, policy-controlled Git content delivery mechanism rather than a direct recreation of the removed `gitRepo` volume plugin.

The strongest value is not cloning Git into a Pod. The value is allowing platform teams to centralise credentials, enforce policy, reduce duplicated workload logic, provide cache-backed startup behaviour, and give security teams visibility into what Git content is consumed by workloads.

The initial implementation should prioritise safety and operability over feature breadth:

* Read-only only.
* No hooks.
* No writable mounts.
* No privileged workload requirements.
* Pinned revisions encouraged or required.
* Submodules optional and policy-gated.
* Credentials centrally managed.
* Strong observability from day one.

This creates a clean separation between developer convenience and platform governance, while avoiding the security model that caused the legacy in-tree plugin to be removed.
