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

// Package kcp is the kcp environment of the integration tests: a sharded kcp
// (front-proxy, cache server, two shards) in Docker via kcp-testcontainers,
// shared by every test binary of the run, or an existing kcp named by
// --kcp-kubeconfig / KCP_KUBECONFIG.
//
//	func TestMain(m *testing.M) { os.Exit(kcp.Main(m)) }
//
//	func TestSomething(t *testing.T) {
//		env := kcp.Server(t)
//		_, path := env.Workspace(t, core.RootCluster.Path(), kcptesting.WithNamePrefix("something"))
//		cl := env.Client(t, path, scheme.New(t))
//		...
//	}
package kcp

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kcptc "github.com/kcp-dev/kcp-testcontainers"
	"github.com/kcp-dev/logicalcluster/v3"
	clusterclient "github.com/kcp-dev/multicluster-provider/client"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	kcptesting "github.com/kcp-dev/sdk/testing"
)

const (
	// KubeconfigEnv names an existing kcp to attach to, like --kcp-kubeconfig.
	KubeconfigEnv = "KCP_KUBECONFIG"
	// ImageEnv is the kcp image (with tag) to run; defaults to
	// kcp-testcontainers' default (ghcr.io/kcp-dev/kcp at its kcp version).
	ImageEnv = "KCP_IMAGE"
	// DefaultInstance is the name of the shared kcp unless WithInstance says
	// otherwise. Test binaries naming the same instance share one kcp.
	DefaultInstance = "platform-mesh"

	baseContext    = "base"
	startupTimeout = 5 * time.Minute

	// The container ports of kcp-testcontainers' components.
	frontProxyPort = "6443"
	shardPort      = "6444"
)

var kubeconfigFlag = flag.String("kcp-kubeconfig", "", "kubeconfig of an existing kcp to run against instead of starting one in Docker (also "+KubeconfigEnv+"); uses context \"base\" if present, else the current context")

// current is the package's kcp, set by Main.
var current *Env

// Env is a running kcp, started in Docker or attached to.
type Env struct {
	config     *rest.Config
	shardNames []string
	attached   bool
	// cluster is used for workspace fixtures; its scheme knows the kcp
	// tenancy and core types only.
	cluster clusterclient.ClusterClient
}

// Option configures Main.
type Option func(*options)

type options struct {
	instance     string
	featureGates []string
}

// WithInstance names the shared kcp this package runs against. Packages
// that need a kcp of their own (because their fixtures clash with the
// others') pick a name nobody else uses.
func WithInstance(name string) Option {
	return func(o *options) { o.instance = name }
}

// WithFeatureGates enables kcp feature gates on the instance. Every package
// sharing the instance must ask for the same gates: whoever starts it wins.
func WithFeatureGates(gates ...string) Option {
	return func(o *options) { o.featureGates = append(o.featureGates, gates...) }
}

// Main is the TestMain of every integration package:
//
//	func TestMain(m *testing.M) { os.Exit(kcp.Main(m)) }
//
// It starts (or joins) the shared kcp in Docker unless --kcp-kubeconfig or
// KCP_KUBECONFIG names an existing kcp, then runs the tests. The containers
// belong to the go test session and are removed by testcontainers' reaper
// once the last test binary of the run has exited.
func Main(m *testing.M, opts ...Option) int {
	if !flag.Parsed() {
		flag.Parse()
	}
	if os.Getenv("TEST_QUIET") != "1" {
		ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	}
	// Keep workspace fixtures until the server goes away unless told otherwise.
	if _, ok := os.LookupEnv("PRESERVE"); !ok {
		_ = os.Setenv("PRESERVE", "true")
	}
	o := options{instance: DefaultInstance}
	for _, opt := range opts {
		opt(&o)
	}

	kubeconfig := *kubeconfigFlag
	if kubeconfig == "" {
		kubeconfig = os.Getenv(KubeconfigEnv)
	}

	var (
		cfg *rest.Config
		err error
	)
	if kubeconfig != "" {
		cfg, err = loadKubeconfig(kubeconfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kcp: loading %s: %v\n", kubeconfig, err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "kcp: attached to %s (%s)\n", cfg.Host, kubeconfig)
	} else {
		cfg, err = start(o)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kcp: %v\n", err)
			return 1
		}
	}

	current, err = newEnv(cfg, kubeconfig != "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "kcp: %v\n", err)
		return 1
	}
	return m.Run()
}

// Server returns the package's kcp; it fails t if Main was not used.
func Server(t testing.TB) *Env {
	t.Helper()
	if current == nil {
		t.Fatal("kcp.Server: no kcp, the package's TestMain must call kcp.Main")
	}
	return current
}

// Config returns the admin rest config through the front-proxy, not scoped
// to a workspace (the "base" context). The caller owns the copy.
func (e *Env) Config() *rest.Config {
	return rest.CopyConfig(e.config)
}

// RESTConfig returns Config scoped to the workspace at path.
func (e *Env) RESTConfig(path logicalcluster.Path) *rest.Config {
	cfg := e.Config()
	cfg.Host = strings.TrimSuffix(cfg.Host, "/") + path.RequestPath()
	return cfg
}

// ClusterClient returns a client that reaches every workspace via Cluster(path).
func (e *Env) ClusterClient(t testing.TB, s *runtime.Scheme) clusterclient.ClusterClient {
	t.Helper()
	cl, err := clusterclient.New(e.Config(), ctrlruntimeclient.Options{Scheme: s})
	require.NoError(t, err, "building kcp cluster client")
	return cl
}

// Client returns a client for the workspace at path.
func (e *Env) Client(t testing.TB, path logicalcluster.Path, s *runtime.Scheme) ctrlruntimeclient.Client {
	t.Helper()
	cl, err := ctrlruntimeclient.New(e.RESTConfig(path), ctrlruntimeclient.Options{Scheme: s})
	require.NoError(t, err, "building client for %s", path)
	return cl
}

// ShardNames returns the names of the kcp shards ("root", "shard-b", ...).
func (e *Env) ShardNames() []string {
	return append([]string(nil), e.shardNames...)
}

// Attached reports whether the kcp was given by --kcp-kubeconfig /
// KCP_KUBECONFIG rather than started by this framework.
func (e *Env) Attached() bool {
	return e.attached
}

// Workspace creates a workspace under parent (type root:universal and name
// prefix "e2e-workspace-" unless opts say otherwise; kcptesting.WithNamePrefix,
// WithType, WithShard, ... apply) and waits until it is Ready and reachable
// by cluster name and by path. The workspace is deleted when t ends unless
// PRESERVE is set, which Main sets by default.
//
// It does what kcptesting.NewWorkspaceFixture does, but polls with
// wait.Eventually and fails t instead of requiring from a goroutine.
func (e *Env) Workspace(t testing.TB, parent logicalcluster.Path, opts ...kcptesting.UnprivilegedWorkspaceOption) (*kcptenancyv1alpha1.Workspace, logicalcluster.Path) {
	t.Helper()
	ws := &kcptenancyv1alpha1.Workspace{
		Spec: kcptenancyv1alpha1.WorkspaceSpec{
			Type: &kcptenancyv1alpha1.WorkspaceTypeReference{
				Name: "universal",
				Path: core.RootCluster.String(),
			},
		},
	}
	for _, opt := range opts {
		opt(ws)
	}
	if ws.Name == "" && ws.GenerateName == "" {
		ws.GenerateName = "e2e-workspace-"
	}
	parentClient := e.cluster.Cluster(parent)

	// A WorkspaceType created a moment ago may not be known to admission yet.
	wait.Eventually(t, func() (bool, string) {
		candidate := ws.DeepCopy()
		if err := parentClient.Create(t.Context(), candidate); err != nil {
			return false, fmt.Sprintf("creating workspace under %s: %v", parent, err)
		}
		ws = candidate
		return true, ""
	}, wait.Timeout, wait.Tick, "%s workspace under %s should be created", ws.Spec.Type.Name, parent)
	path := parent.Join(ws.Name)

	t.Cleanup(func() {
		if os.Getenv("PRESERVE") != "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := parentClient.Delete(ctx, ws)
		if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsForbidden(err) {
			t.Errorf("deleting workspace %s: %v", path, err)
		}
	})

	wait.Eventually(t, func() (bool, string) {
		err := parentClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: ws.Name}, ws)
		if apierrors.IsNotFound(err) {
			return false, fmt.Sprintf("workspace %s was deleted", path)
		}
		if err != nil {
			return false, fmt.Sprintf("getting workspace %s: %v", path, err)
		}
		if ws.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
			return false, fmt.Sprintf("workspace phase is %q, not Ready", ws.Status.Phase)
		}
		return true, ""
	}, wait.Timeout, wait.Tick, "workspace %s should be Ready", path)

	wait.Eventually(t, func() (bool, string) {
		lc := &kcpcorev1alpha1.LogicalCluster{}
		key := ctrlruntimeclient.ObjectKey{Name: kcpcorev1alpha1.LogicalClusterName}
		if err := e.cluster.Cluster(logicalcluster.NewPath(ws.Spec.Cluster)).Get(t.Context(), key, lc); err != nil {
			return false, fmt.Sprintf("getting LogicalCluster of %s by cluster name %s: %v", path, ws.Spec.Cluster, err)
		}
		if err := e.cluster.Cluster(path).Get(t.Context(), key, lc); err != nil {
			return false, fmt.Sprintf("getting LogicalCluster of %s by path: %v", path, err)
		}
		return true, ""
	}, wait.Timeout, wait.Tick, "workspace %s should be reachable", path)

	t.Logf("Created %s workspace %s as /clusters/%s", ws.Spec.Type.Name, path, ws.Spec.Cluster)
	return ws, path
}

// start starts the shared kcp named o.instance in Docker, or joins it when
// another test binary of this go test session started it already, and
// returns the unscoped admin config.
//
// kcp-testcontainers wires the components over a Docker network and makes
// the shards advertise themselves under their network aliases, which the
// host cannot resolve. The tests run the operators in-process on the host,
// and those follow the URLs kcp advertises (workspace URLs, the APIExport
// virtual workspace URLs in APIExportEndpointSlices), so the front-proxy and
// both shards get a fixed host port each and the shards advertise the host
// side: --shard-external-url for workspace URLs, --shard-virtual-workspace-url
// for virtual workspaces. Every serving certificate carries "localhost", so
// the configs verify against the URL's host instead of the alias.
func start(o options) (*rest.Config, error) {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()

	host, err := daemonHost(ctx)
	if err != nil {
		return nil, err
	}
	ports, err := freePorts(3)
	if err != nil {
		return nil, err
	}
	frontProxy, shardA, shardB := ports[0], ports[1], ports[2]
	external := "https://" + net.JoinHostPort(host, strconv.Itoa(frontProxy))

	logDir, err := logDir(o.instance)
	if err != nil {
		return nil, err
	}

	image := os.Getenv(ImageEnv)
	if image == "" {
		image = kcptc.DefaultTag
	}
	fmt.Fprintf(os.Stderr, "kcp: starting or joining instance %q (%s), logs in %s\n", o.instance, image, logDir)
	startedAt := time.Now()
	instance, err := kcptc.Shared(ctx, o.instance, kcptc.Options{
		Image:          image,
		StartupTimeout: startupTimeout,
		FeatureGates:   o.featureGates,
		Cache: []testcontainers.ContainerCustomizer{
			logTo(logDir, kcptc.AliasCache),
		},
		FrontProxy: []testcontainers.ContainerCustomizer{
			publish(frontProxy, frontProxyPort),
			logTo(logDir, kcptc.AliasFrontProxy),
		},
		ShardA: []testcontainers.ContainerCustomizer{
			publish(shardA, shardPort),
			advertise(external, "https://"+net.JoinHostPort(host, strconv.Itoa(shardA))),
			logTo(logDir, kcptc.ShardA),
		},
		ShardB: []testcontainers.ContainerCustomizer{
			publish(shardB, shardPort),
			advertise(external, "https://"+net.JoinHostPort(host, strconv.Itoa(shardB))),
			logTo(logDir, kcptc.ShardB),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("starting kcp %q in Docker: %w", o.instance, err)
	}

	cfg := instance.Config()
	if i := strings.Index(cfg.Host, "/clusters/"); i >= 0 {
		cfg.Host = cfg.Host[:i]
	}
	// Verify against the host in the URL ("localhost" is in every serving
	// cert), not the component's network alias: the virtual workspace URLs
	// point at the shards, whose certs do not carry the front-proxy's name.
	cfg.ServerName = ""
	fmt.Fprintf(os.Stderr, "kcp: %s ready after %s\n", cfg.Host, time.Since(startedAt).Round(100*time.Millisecond))
	return cfg, nil
}

// publish binds containerPort to hostPort on the Docker host (testcontainers
// keeps the binding and maps the other exposed ports randomly).
func publish(hostPort int, containerPort string) testcontainers.CustomizeRequestOption {
	return testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
		hc.PortBindings = network.PortMap{
			network.MustParsePort(containerPort + "/tcp"): {{HostPort: strconv.Itoa(hostPort)}},
		}
	})
}

// advertise makes a shard tell clients to reach it (and its virtual
// workspaces) at host-side URLs instead of its Docker network alias.
func advertise(externalURL, virtualWorkspaceURL string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		cmd := make([]string, 0, len(req.Cmd)+1)
		for _, arg := range req.Cmd {
			switch {
			case strings.HasPrefix(arg, "--shard-external-url="):
				arg = "--shard-external-url=" + externalURL
			case strings.HasPrefix(arg, "--shard-virtual-workspace-url="):
				continue
			}
			cmd = append(cmd, arg)
		}
		req.Cmd = append(cmd, "--shard-virtual-workspace-url="+virtualWorkspaceURL)
		return nil
	}
}

// logTo streams the container's output to <dir>/<component>.log.
func logTo(dir, component string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		f, err := os.Create(filepath.Join(dir, component+".log"))
		if err != nil {
			return fmt.Errorf("creating log file for %s: %w", component, err)
		}
		req.LogConsumerCfg = &testcontainers.LogConsumerConfig{Consumers: []testcontainers.LogConsumer{fileLog{f}}}
		return nil
	}
}

// fileLog writes container output to a file; it is never closed, the
// process's exit does that.
type fileLog struct{ f *os.File }

func (l fileLog) Accept(entry testcontainers.Log) {
	_, _ = l.f.Write(entry.Content)
}

// daemonHost is the host under which the Docker daemon publishes container
// ports ("localhost" for a local daemon).
func daemonHost(ctx context.Context) (string, error) {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return "", fmt.Errorf("connecting to Docker: %w", err)
	}
	host, err := provider.DaemonHost(ctx)
	if err != nil {
		_ = provider.Close()
		return "", fmt.Errorf("resolving the Docker host: %w", err)
	}
	return host, provider.Close()
}

// freePorts picks n free TCP ports on the host. They are released again
// before Docker binds them, so another process could grab one in between;
// the start then fails and the run is simply repeated.
func freePorts(n int) ([]int, error) {
	listeners := make([]net.Listener, 0, n)
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()
	ports := make([]int, 0, n)
	for range n {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("picking a free port: %w", err)
		}
		listeners = append(listeners, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports, nil
}

// logDir is <repo>/tests/.test/<instance>, created if needed.
func logDir(instance string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, err := findModuleRoot(cwd)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "tests", ".test", instance)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating log dir %s: %w", dir, err)
	}
	return dir, nil
}

// findModuleRoot walks up from dir to the closest go.mod and returns its
// directory.
func findModuleRoot(dir string) (string, error) {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
	}
}

// loadKubeconfig loads the "base" context if the kubeconfig has one, else
// the current context. A /clusters/<path> suffix on the server is dropped so
// the config is unscoped like the Docker instance's.
func loadKubeconfig(path string) (*rest.Config, error) {
	raw, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, err
	}
	ctxName := raw.CurrentContext
	if _, ok := raw.Contexts[baseContext]; ok {
		ctxName = baseContext
	}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(*raw, ctxName, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
	if err != nil {
		return nil, err
	}
	if i := strings.Index(cfg.Host, "/clusters/"); i >= 0 {
		cfg.Host = cfg.Host[:i]
	}
	return cfg, nil
}

func newEnv(cfg *rest.Config, attached bool) (*Env, error) {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(s))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(s))

	cluster, err := clusterclient.New(cfg, ctrlruntimeclient.Options{Scheme: s})
	if err != nil {
		return nil, fmt.Errorf("building cluster client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	shards := &kcpcorev1alpha1.ShardList{}
	if err := cluster.Cluster(core.RootCluster.Path()).List(ctx, shards); err != nil {
		return nil, fmt.Errorf("listing shards: %w", err)
	}
	names := make([]string, 0, len(shards.Items))
	for _, shard := range shards.Items {
		names = append(names, shard.Name)
	}

	return &Env{config: cfg, shardNames: names, attached: attached, cluster: cluster}, nil
}
