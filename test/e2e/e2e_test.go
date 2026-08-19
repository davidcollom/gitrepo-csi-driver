//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	nodePluginImage = "gitrepo-csi-nodeplugin:e2e"
	gitServerImage  = "gitrepo-csi-gitserver:e2e"
	registrarImage  = "registry.k8s.io/sig-storage/csi-node-driver-registrar:v2.13.0"
)

func TestKindCSIEphemeralVolumeMaterializesBranchTagAndCommitRefs(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 and run with -tags=e2e to create a kind cluster")
	}

	requireTool(t, "docker")
	requireTool(t, "git")
	requireTool(t, "go")
	requireTool(t, "helm")
	requireTool(t, "kind")
	requireTool(t, "kubectl")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	root := repoRoot(t)
	tmp := t.TempDir()
	arch := dockerArch(t, ctx)
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	clusterName := getenv("KIND_CLUSTER_NAME", "gitrepo-csi-e2e-"+runID)
	namespace := getenv("E2E_NAMESPACE", "gitrepo-csi-workload-"+runID)
	systemNamespace := getenv("E2E_SYSTEM_NAMESPACE", "gitrepo-csi-system-"+runID)
	reuseCluster := os.Getenv("E2E_REUSE_CLUSTER") == "1"

	fixture := createGitFixture(t, ctx, tmp)
	buildNodePluginImage(t, ctx, root, tmp, arch)
	buildGitServerImage(t, ctx, root, tmp, arch, fixture.BareRepo)
	if !reuseCluster {
		run(t, ctx, root, nil, "kind", "create", "cluster", "--name", clusterName, "--wait", "90s")
		t.Cleanup(func() {
			if os.Getenv("E2E_KEEP_CLUSTER") == "1" {
				return
			}
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cleanupCancel()
			_ = exec.CommandContext(cleanupCtx, "kind", "delete", "cluster", "--name", clusterName).Run()
		})
	}

	run(t, ctx, root, nil, "kind", "load", "docker-image", "--name", clusterName, nodePluginImage)
	run(t, ctx, root, nil, "kind", "load", "docker-image", "--name", clusterName, gitServerImage)

	kubectl := []string{"--context", "kind-" + clusterName}
	run(t, ctx, root, nil, "kubectl", append(kubectl, "delete", "namespace", namespace, "--ignore-not-found=true")...)
	applyYAML(t, ctx, root, kubectl, namespaceYAML(namespace))
	applyYAML(t, ctx, root, kubectl, gitServerYAML(namespace))
	waitForDeployment(t, ctx, root, kubectl, namespace, "git-fixture")

	repoURL := fmt.Sprintf("http://git-fixture.%s.svc.cluster.local:8080/repo.git", namespace)
	valuesPath := filepath.Join(tmp, "values.yaml")
	writeFile(t, valuesPath, helmValues(namespace, repoURL))
	helmArgs := []string{
		"upgrade", "--install", "gitrepo-csi-driver", filepath.Join(root, "helm/gitrepo-csi-driver"),
		"--namespace", systemNamespace,
		"--create-namespace",
		"--values", valuesPath,
		"--kube-context", "kind-" + clusterName,
		"--wait",
		"--timeout", "2m",
	}
	run(t, ctx, root, nil, "helm", helmArgs...)
	waitForDaemonSet(t, ctx, root, kubectl, systemNamespace, "gitrepo-csi-driver-nodeplugin")
	nodePluginPod := kubectlOutput(t, ctx, root, kubectl, "get", "pods", "-n", systemNamespace, "-l", "app.kubernetes.io/component=nodeplugin", "-o", "jsonpath={.items[0].metadata.name}")
	waitForHTTPFromPod(t, ctx, root, kubectl, systemNamespace, nodePluginPod, "nodeplugin", fmt.Sprintf("http://git-fixture.%s.svc.cluster.local:8080/healthz", namespace))

	cases := []struct {
		name         string
		revision     string
		revisionKind string
		target       string
		wantContent  string
		wantCommit   string
	}{
		{
			name:         "branch",
			revision:     "refs/heads/feature/e2e",
			revisionKind: "branch",
			target:       "branch",
			wantContent:  "branch fixture\n",
			wantCommit:   fixture.BranchCommit,
		},
		{
			name:         "tag",
			revision:     "refs/tags/v1.0.0",
			revisionKind: "tag",
			target:       "tag",
			wantContent:  "tag fixture\n",
			wantCommit:   fixture.TagCommit,
		},
		{
			name:        "commit",
			revision:    fixture.Commit,
			target:      "commit",
			wantContent: "commit fixture\n",
			wantCommit:  fixture.Commit,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			podName := "gitcontent-" + tc.target
			applyYAML(t, ctx, root, kubectl, csiWorkloadYAML(namespace, podName, repoURL, tc.revision, tc.revisionKind))
			waitForPodReady(t, ctx, root, kubectl, namespace, podName, systemNamespace)

			gotContent := kubectlOutput(t, ctx, root, kubectl, "exec", "-n", namespace, podName, "--", "cat", "/content/content.txt")
			if gotContent != tc.wantContent {
				t.Fatalf("mounted content = %q, want %q", gotContent, tc.wantContent)
			}
			kubectlOutput(t, ctx, root, kubectl, "exec", "-n", namespace, podName, "--", "test", "!", "-e", "/content/.git")

			gotRevision := kubectlOutput(t, ctx, root, kubectl, "exec", "-n", namespace, podName, "--", "cat", "/content/.gitcontent/resolved-revision")
			if gotRevision != tc.wantCommit+"\n" {
				t.Fatalf("metadata resolved revision = %q, want %q", gotRevision, tc.wantCommit+"\n")
			}
		})
	}
}

type fixtureRepo struct {
	BareRepo     string
	TagCommit    string
	BranchCommit string
	Commit       string
}

func createGitFixture(t *testing.T, ctx context.Context, tmp string) fixtureRepo {
	t.Helper()

	workTree := filepath.Join(tmp, "fixture-worktree")
	bareRepo := filepath.Join(tmp, "gitserver-context", "repo.git")
	mustMkdir(t, filepath.Dir(bareRepo))
	mustMkdir(t, workTree)

	git := func(args ...string) string {
		t.Helper()
		return run(t, ctx, workTree, nil, "git", args...)
	}

	git("init", "-b", "main")
	git("config", "user.name", "E2E")
	git("config", "user.email", "e2e@example.invalid")
	writeFile(t, filepath.Join(workTree, "content.txt"), "tag fixture\n")
	git("add", "content.txt")
	git("commit", "-m", "tag fixture")
	tagCommit := strings.TrimSpace(git("rev-parse", "HEAD"))
	git("tag", "v1.0.0")

	git("checkout", "-b", "feature/e2e")
	writeFile(t, filepath.Join(workTree, "content.txt"), "branch fixture\n")
	git("commit", "-am", "branch fixture")
	branchCommit := strings.TrimSpace(git("rev-parse", "HEAD"))

	git("checkout", "main")
	writeFile(t, filepath.Join(workTree, "content.txt"), "commit fixture\n")
	git("commit", "-am", "commit fixture")
	commit := strings.TrimSpace(git("rev-parse", "HEAD"))

	run(t, ctx, tmp, nil, "git", "clone", "--bare", workTree, bareRepo)
	run(t, ctx, bareRepo, nil, "git", "update-server-info")

	return fixtureRepo{
		BareRepo:     bareRepo,
		TagCommit:    tagCommit,
		BranchCommit: branchCommit,
		Commit:       commit,
	}
}

func buildNodePluginImage(t *testing.T, ctx context.Context, root, tmp, arch string) {
	t.Helper()

	contextDir := filepath.Join(tmp, "nodeplugin-context")
	mustMkdir(t, contextDir)
	bin := filepath.Join(contextDir, "gitrepo-csi-nodeplugin")
	env := []string{"CGO_ENABLED=0", "GOOS=linux", "GOARCH=" + arch}
	run(t, ctx, root, env, "go", "build", "-o", bin, "./cmd/nodeplugin")
	run(t, ctx, root, nil, "docker", "build", "-f", filepath.Join(root, "test/e2e/testdata/nodeplugin.Dockerfile"), "-t", nodePluginImage, contextDir)
}

func buildGitServerImage(t *testing.T, ctx context.Context, root, tmp, arch, bareRepo string) {
	t.Helper()

	contextDir := filepath.Join(tmp, "gitserver-context")
	env := []string{"CGO_ENABLED=0", "GOOS=linux", "GOARCH=" + arch}
	run(t, ctx, root, env, "go", "build", "-o", filepath.Join(contextDir, "git-http-server"), "./test/e2e/testdata/gitserver")
	if filepath.Dir(bareRepo) != contextDir {
		t.Fatalf("fixture bare repo must be in gitserver build context: %s", bareRepo)
	}
	run(t, ctx, root, nil, "docker", "build", "-f", filepath.Join(root, "test/e2e/testdata/gitserver.Dockerfile"), "-t", gitServerImage, contextDir)
}

func waitForDeployment(t *testing.T, ctx context.Context, root string, kubectl []string, namespace, name string) {
	t.Helper()
	kubectlOutput(t, ctx, root, kubectl, "rollout", "status", "deployment/"+name, "-n", namespace, "--timeout=90s")
}

func waitForDaemonSet(t *testing.T, ctx context.Context, root string, kubectl []string, namespace, name string) {
	t.Helper()
	kubectlOutput(t, ctx, root, kubectl, "rollout", "status", "daemonset/"+name, "-n", namespace, "--timeout=120s")
}

func waitForPodReady(t *testing.T, ctx context.Context, root string, kubectl []string, namespace, pod, systemNamespace string) {
	t.Helper()
	if _, err := kubectlCombinedOutput(ctx, root, kubectl, "wait", "--for=condition=Ready", "pod/"+pod, "-n", namespace, "--timeout=90s"); err != nil {
		dumpPodDiagnostics(t, ctx, root, kubectl, namespace, pod, systemNamespace)
		t.Fatalf("pod %s/%s did not become Ready: %v", namespace, pod, err)
	}
}

func dumpPodDiagnostics(t *testing.T, ctx context.Context, root string, kubectl []string, namespace, pod, systemNamespace string) {
	t.Helper()
	logKubectl(t, ctx, root, kubectl, "describe", "pod", "-n", namespace, pod)
	logKubectl(t, ctx, root, kubectl, "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
	logKubectl(t, ctx, root, kubectl, "get", "pods", "-n", systemNamespace, "-o", "wide")
	logKubectl(t, ctx, root, kubectl, "logs", "-n", systemNamespace, "-l", "app.kubernetes.io/component=nodeplugin", "-c", "nodeplugin", "--tail=200")
}

func logKubectl(t *testing.T, ctx context.Context, root string, kubectl []string, args ...string) {
	t.Helper()
	out, err := kubectlCombinedOutput(ctx, root, kubectl, args...)
	if err != nil {
		t.Logf("kubectl %s failed: %v\n%s", strings.Join(args, " "), err, out)
		return
	}
	t.Logf("kubectl %s:\n%s", strings.Join(args, " "), out)
}

func waitForHTTPFromPod(t *testing.T, ctx context.Context, root string, kubectl []string, namespace, pod, container, url string) {
	t.Helper()

	deadline := time.Now().Add(45 * time.Second)
	var lastOutput string
	for time.Now().Before(deadline) {
		args := append(append([]string{}, kubectl...), "exec", "-n", namespace, pod, "-c", container, "--", "wget", "-q", "-O-", url)
		cmd := exec.CommandContext(ctx, "kubectl", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		lastOutput = string(out)
		if err == nil && strings.TrimSpace(lastOutput) == "ok" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s from %s/%s, last output: %s", url, pod, container, lastOutput)
}

func applyYAML(t *testing.T, ctx context.Context, root string, kubectl []string, manifest string) {
	t.Helper()
	args := append(append([]string{}, kubectl...), "apply", "-f", "-")
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(manifest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl apply failed: %v\n%s\nmanifest:\n%s", err, string(out), manifest)
	}
}

func kubectlOutput(t *testing.T, ctx context.Context, root string, kubectl []string, args ...string) string {
	t.Helper()
	out, err := kubectlCombinedOutput(ctx, root, kubectl, args...)
	if err != nil {
		t.Fatalf("kubectl %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func kubectlCombinedOutput(ctx context.Context, root string, kubectl []string, args ...string) (string, error) {
	kubectlArgs := append(append([]string{}, kubectl...), args...)
	cmd := exec.CommandContext(ctx, "kubectl", kubectlArgs...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func run(t *testing.T, ctx context.Context, dir string, extraEnv []string, name string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func dockerArch(t *testing.T, ctx context.Context) string {
	t.Helper()

	arch := strings.TrimSpace(run(t, ctx, "", nil, "docker", "info", "--format", "{{.Architecture}}"))
	switch arch {
	case "amd64", "arm64":
		return arch
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
			return runtime.GOARCH
		}
		t.Fatalf("unsupported docker architecture %q", arch)
		return ""
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func namespaceYAML(namespace string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, namespace)
}

func gitServerYAML(namespace string) string {
	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: git-fixture
  namespace: %[1]s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: git-fixture
  template:
    metadata:
      labels:
        app: git-fixture
    spec:
      containers:
        - name: git
          image: %[2]s
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: 8080
          readinessProbe:
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 1
            periodSeconds: 1
            timeoutSeconds: 1
---
apiVersion: v1
kind: Service
metadata:
  name: git-fixture
  namespace: %[1]s
spec:
  selector:
    app: git-fixture
  ports:
    - port: 8080
      targetPort: 8080
`, namespace, gitServerImage)
}

func policyYAML(namespace, repoURL string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(repoURL, "http://"), "https://")
	host = strings.TrimSuffix(strings.Split(host, "/")[0], ":8080")
	return fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: gitcontent-policy
  namespace: %[1]s
data:
  policies.yaml: |
    policies:
      - name: e2e
        namespaces:
          - %[1]s
        allowedRepositories:
          - %[2]s
        allowedHosts:
          - %[3]s
        revisions:
          requirePinnedCommit: false
          allowBranches: true
          allowTags: true
          allowedBranchPatterns:
            - '^refs/heads/feature/e2e$'
          allowedTagPatterns:
            - '^refs/tags/v1\.0\.0$'
        clone:
          defaultDepth: 1
          maxDepth: 5
          timeout: 60s
          maxRepositorySize: 10485760
          maxFileCount: 1000
          allowSparseCheckout: true
        submodules:
          enabled: false
          recursive: false
          maxDepth: 1
        lfs:
          enabled: false
`, namespace, repoURL, host)
}

func helmValues(namespace, repoURL string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(repoURL, "http://"), "https://")
	host = strings.TrimSuffix(strings.Split(host, "/")[0], ":8080")
	return fmt.Sprintf(`fullnameOverride: gitrepo-csi-driver
policy:
  content: |
    policies:
      - name: e2e
        namespaces:
          - %[1]s
        allowedRepositories:
          - %[2]s
        allowedHosts:
          - %[3]s
        revisions:
          requirePinnedCommit: false
          allowBranches: true
          allowTags: true
          allowedBranchPatterns:
            - '^refs/heads/feature/e2e$'
          allowedTagPatterns:
            - '^refs/tags/v1\.0\.0$'
        clone:
          defaultDepth: 1
          maxDepth: 5
          timeout: 60s
          maxRepositorySize: 10485760
          maxFileCount: 1000
          allowSparseCheckout: true
        submodules:
          enabled: false
          recursive: false
          maxDepth: 1
        lfs:
          enabled: false
nodePlugin:
  image:
    repository: gitrepo-csi-nodeplugin
    tag: e2e
    pullPolicy: IfNotPresent
  registrar:
    image:
      repository: registry.k8s.io/sig-storage/csi-node-driver-registrar
      tag: v2.13.0
      pullPolicy: IfNotPresent
admissionWebhook:
  enabled: false
`, namespace, repoURL, host)
}

func csiWorkloadYAML(namespace, name, repoURL, revision, revisionKind string) string {
	revisionKindBlock := ""
	if revisionKind != "" {
		revisionKindBlock = fmt.Sprintf("          revisionKind: %s\n", revisionKind)
	}
	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %[2]s
  namespace: %[1]s
spec:
  restartPolicy: Never
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    runAsGroup: 1000
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: verifier
      image: %[3]s
      imagePullPolicy: IfNotPresent
      command:
        - /bin/sh
        - -c
        - sleep 3600
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop:
            - ALL
      volumeMounts:
        - name: content
          mountPath: /content
          readOnly: true
  volumes:
    - name: content
      csi:
        driver: gitcontent.csi.example.io
        readOnly: true
        volumeAttributes:
          repo: %[4]s
          revision: %[5]s
%[6]s          policy: e2e
`, namespace, name, nodePluginImage, repoURL, revision, revisionKindBlock)
}

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("required tool %q not found in PATH", name)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
