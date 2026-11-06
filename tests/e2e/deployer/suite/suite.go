/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package suite provides a harness to setup kind clusters for deployer e2e tests.
package suite

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh-deployer/pkg/config"
	"go.platform-mesh.io/platform-mesh-deployer/pkg/deployer"
	"go.platform-mesh.io/platform-mesh-deployer/pkg/kcp"
	"go.platform-mesh.io/platform-mesh-deployer/pkg/ocm"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/providers/multi"
)

const (
	// ProviderNamespace is where the deployer watches kubeconfig secrets and creates admin CRs.
	ProviderNamespace = "platform-mesh-system"

	// PlatformMeshName is the installation every e2e fixture deploys.
	PlatformMeshName = "customer-a"

	kindClusterPrefix = "pm-deployer-e2e"
)

// Cluster is a started kind cluster.
type Cluster struct {
	Name   string
	Config *rest.Config
	Client ctrlruntimeclient.Client
	// NodeIP is the node's address on the shared kind docker network, used as the clusterID so sslip.io hosts resolve.
	NodeIP string
}

// Env is a started e2e environment: a config plane plus workload clusters.
type Env struct {
	Config    *Cluster
	Workloads []*Cluster
	// registryAddr is the forwarded address of the OCM registry, set by
	// InstallRegistry.
	registryAddr string
}

// Start provisions the config plane and workloadClusters workload clusters.
func Start(t *testing.T, workloadClusters int) *Env {
	t.Helper()

	cfgPlane := createCluster(t, "config")
	createNamespace(t, cfgPlane.Client, ProviderNamespace)
	applyKustomize(t, cfgPlane, base("crd"))
	// The deployer runs kcp-operator's controllers itself, so only its CRDs are
	// installed, on the config plane and on every workload cluster.
	applyKustomize(t, cfgPlane, base("bases", "kcp-operator", "crds"))
	applyKustomize(t, cfgPlane, base("bases", "cert-manager"))
	rolloutWait(t, cfgPlane, "cert-manager", "deployment/cert-manager")
	rolloutWait(t, cfgPlane, "cert-manager", "deployment/cert-manager-cainjector")
	rolloutWait(t, cfgPlane, "cert-manager", "deployment/cert-manager-webhook")
	applyKustomizeRetry(t, cfgPlane, base("bases", "cert-issuer"))
	installEnvoyGateway(t, cfgPlane)
	applyKustomizeNS(t, cfgPlane, base("bases", "etcd-tls"), ProviderNamespace)
	setupEtcdTLS(t, cfgPlane)

	env := &Env{Config: cfgPlane}
	if workloadClusters == 0 {
		env.Workloads = []*Cluster{cfgPlane}
	} else {
		for i := range workloadClusters {
			w := createCluster(t, fmt.Sprintf("workload-%d", i))
			installEnvoyGateway(t, w)
			applyKustomize(t, w, base("bases", "kcp-operator", "crds"))
			env.Workloads = append(env.Workloads, w)
		}
	}

	startDeployer(t, env)
	t.Cleanup(func() {
		if t.Failed() {
			dumpDiagnostics(t, cfgPlane)
		}
	})
	return env
}

// EngageWorkload writes a kubeconfig Secret pointing at workload, labeled for the given components, onto the config plane so the deployer engages it.
// The clusterID is the workload's dashed node IP so sslip.io hosts resolve.
func (e *Env) EngageWorkload(t *testing.T, platformMesh string, workload *Cluster, components ...string) {
	t.Helper()
	kubeconfig, err := restToKubeconfig(workload.Config)
	require.NoError(t, err)

	labels := map[string]string{}
	for _, c := range components {
		labels["deploy.platform-mesh.io/"+c] = "true"
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      platformMesh + "--" + workload.NodeIP,
			Namespace: ProviderNamespace,
			Labels:    labels,
		},
		Data: map[string][]byte{"kubeconfig": kubeconfig},
	}
	require.NoError(t, e.Config.Client.Create(t.Context(), secret))
}

func createCluster(t *testing.T, role string) *Cluster {
	t.Helper()
	name := kindClusterPrefix + "-" + slug(t.Name()) + "-" + role
	kubeconfig := filepath.Join(t.TempDir(), name+".kubeconfig")

	// hack/kind.sh creates the cluster, writes its kubeconfig and makes
	// CoreDNS answer <name>.<ip>.sslip.io in-cluster.
	sh(t, kindScript(), "up", "--name", name, "--kubeconfig", kubeconfig)
	t.Cleanup(func() {
		if os.Getenv("E2E_KEEP") != "" {
			t.Logf("E2E_KEEP set: leaving cluster %s (kubeconfig %s)", name, kubeconfig)
			return
		}
		_ = exec.Command(kindScript(), "down", "--name", name, "--kubeconfig", kubeconfig).Run() //nolint:gosec // test-controlled args
	})

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err)
	cl, err := ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: deployer.NewScheme()})
	require.NoError(t, err)
	c := &Cluster{Name: name, Config: cfg, Client: cl}
	c.NodeIP = dashedIP(nodeInternalIP(t, c))
	return c
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// slug turns a test name into a cluster name component, so tests running in
// parallel do not collide on the same kind cluster.
func slug(name string) string {
	name = nonAlnum.ReplaceAllString(strings.ToLower(strings.TrimPrefix(name, "Test")), "-")
	return strings.Trim(name, "-")
}

func startDeployer(t *testing.T, e *Env) {
	c := e.Config
	t.Helper()
	// Without this the deployer's own reconcile errors are silently dropped,
	// leaving a failing test with nothing to go on. Still needed next to the
	// manager's own logger below, for the components built outside it.
	setLogger()
	provider := multi.New(multi.Options{})
	mgr, err := mcmanager.New(c.Config, provider, mcmanager.Options{
		Scheme:                 deployer.NewScheme(),
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		// Tests run in parallel, so name the manager's logs after the test
		// that owns them.
		Logger: zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr)).WithName(t.Name()),
		// Controller names are unique per process, not per manager, so every
		// test after the first would fail to start its cluster providers.
		Controller: ctrlconfig.Controller{SkipNameValidation: ptr.To(true)},
	})
	require.NoError(t, err)

	opCfg := config.NewOperatorConfig()
	cfg := opCfg.DeployerConfig(mgr, nil, ocm.New())
	// The deployer runs on the host here, where the front proxy's sslip.io
	// hostname is blocked by DNS rebind protection and the node address is
	// not routable on every container runtime.
	cfg.KcpDial = FrontProxyDialer(t, c)
	// The kcp-operator controllers address the shards as well, so they go
	// through the gateway, which routes to either by SNI.
	cfg.KcpAddress = kcp.Addresser{Dial: e.GatewayDialer(t)}
	require.NoError(t, deployer.AddProviders(provider, mgr, cfg))
	require.NoError(t, deployer.Setup(mgr, cfg))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		if err := mgr.Start(ctx); err != nil {
			t.Errorf("manager stopped: %v", err)
		}
	}()
}

func createNamespace(t *testing.T, cl ctrlruntimeclient.Client, name string) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := cl.Create(t.Context(), ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("creating namespace %q: %v", name, err)
	}
}

// repoRoot is the root of the platform-mesh repository, resolved from this
// source file's location (tests/e2e/deployer/suite), so the suite does not
// depend on the working directory go test runs in.
func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}

// kindScript is the repository's hack/kind.sh. It honours KIND and KUBECTL
// for the binaries.
func kindScript() string {
	return filepath.Join(repoRoot(), "hack", "kind.sh")
}

// kindBinary is the kind binary hack/kind.sh uses too: $KIND, else kind on PATH.
func kindBinary() string {
	if kind := os.Getenv("KIND"); kind != "" {
		return kind
	}
	return "kind"
}

// base resolves a path under the deployer's config/ directory
// (operators/platform-mesh-deployer/config).
func base(elem ...string) string {
	root := filepath.Join(repoRoot(), "operators", "platform-mesh-deployer", "config")
	return filepath.Join(append([]string{root}, elem...)...)
}

func applyKustomize(t *testing.T, c *Cluster, kustomization string) {
	t.Helper()
	kubectlRun(t, c.Config, "apply", "-k", kustomization, "--server-side", "--force-conflicts")
}

// applyKustomizeRetry applies a kustomization, retrying transient errors such as
// the cert-manager webhook not yet serving.
func applyKustomizeRetry(t *testing.T, c *Cluster, kustomization string) {
	t.Helper()
	path := writeKubeconfig(t, c.Config)
	var out []byte
	var err error
	for range 20 {
		out, err = exec.Command("kubectl", "--kubeconfig", path, "apply", "-k", kustomization, "--server-side", "--force-conflicts").CombinedOutput() //nolint:gosec // test-controlled args
		if err == nil {
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("kubectl apply -k %s:\n%s", kustomization, out)
}

func applyKustomizeNS(t *testing.T, c *Cluster, kustomization, namespace string) {
	t.Helper()
	kubectlRun(t, c.Config, "apply", "-k", kustomization, "-n", namespace, "--server-side", "--force-conflicts")
}

// applyYAML applies inline YAML via kubectl (for manifests templated at runtime).
func applyYAML(t *testing.T, c *Cluster, yaml string) {
	t.Helper()
	path := writeKubeconfig(t, c.Config)
	cmd := exec.Command("kubectl", "--kubeconfig", path, "apply", "--server-side", "--force-conflicts", "-f", "-") //nolint:gosec // test-controlled args
	cmd.Stdin = strings.NewReader(yaml)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("kubectl apply -f -:\n%s\n%s", yaml, out)
	}
}

// waitForSecret polls until the named secret exists (cert-manager issuance is async).
func waitForSecret(t *testing.T, c *Cluster, namespace, name string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return c.Client.Get(t.Context(), ctrlruntimeclient.ObjectKey{Namespace: namespace, Name: name}, &corev1.Secret{}) == nil
	}, 3*time.Minute, 2*time.Second, "secret %s/%s not created", namespace, name)
}

func rolloutWait(t *testing.T, c *Cluster, namespace, object string) {
	t.Helper()
	kubectlRun(t, c.Config, "-n", namespace, "rollout", "status", object, "--timeout=180s")
}

func kubectlRun(t *testing.T, cfg *rest.Config, args ...string) {
	t.Helper()
	path := writeKubeconfig(t, cfg)
	sh(t, "kubectl", append([]string{"--kubeconfig", path}, args...)...)
}

func writeKubeconfig(t *testing.T, cfg *rest.Config) string {
	t.Helper()
	kubeconfig, err := restToKubeconfig(cfg)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, kubeconfig, 0o600))
	return path
}

func sh(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...) //nolint:gosec // test-controlled args
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v:\n%s", name, args, out)
	}
}

// dumpDiagnostics logs the config-plane state to help debug a failed pipeline.
func dumpDiagnostics(t *testing.T, c *Cluster) {
	t.Helper()
	kubeconfig, err := restToKubeconfig(c.Config)
	if err != nil {
		return
	}
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, kubeconfig, 0o600); err != nil {
		return
	}
	for _, args := range [][]string{
		{"-n", ProviderNamespace, "get", "rootshards,compiledrootshards,frontproxies,compiledfrontproxies,shards,compiledshards,cacheservers,compiledcacheservers,virtualworkspaces,compiledvirtualworkspaces"},
		{"-n", ProviderNamespace, "get", "certificates,issuers,clusterissuers,secrets"},
		{"-n", ProviderNamespace, "get", "events", "--sort-by=.lastTimestamp"},
	} {
		out, _ := exec.Command("kubectl", append([]string{"--kubeconfig", path}, args...)...).CombinedOutput() //nolint:gosec // test-controlled args
		t.Logf("=== kubectl %v ===\n%s", args, out)
	}
}

func restToKubeconfig(cfg *rest.Config) ([]byte, error) {
	c := clientcmdapi.NewConfig()
	c.Clusters["default"] = &clientcmdapi.Cluster{
		Server:                   cfg.Host,
		CertificateAuthorityData: cfg.CAData,
	}
	c.AuthInfos["default"] = &clientcmdapi.AuthInfo{
		ClientCertificateData: cfg.CertData,
		ClientKeyData:         cfg.KeyData,
		Token:                 cfg.BearerToken,
	}
	c.Contexts["default"] = &clientcmdapi.Context{Cluster: "default", AuthInfo: "default"}
	c.CurrentContext = "default"
	return clientcmd.Write(*c)
}

var setLogger = sync.OnceFunc(func() {
	ctrllog.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr)))
})
