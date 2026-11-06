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

// Package kcp is the kcp environment of the integration tests: a private
// sharded-test-server per test binary (front-proxy, cache server, two
// shards), or an existing kcp named by --kcp-kubeconfig / KCP_KUBECONFIG.
//
//	func TestMain(m *testing.M) { os.Exit(kcp.Main(m)) }
//
//	func TestSomething(t *testing.T) {
//		env := kcp.Server(t)
//		_, path := env.Workspace(t, core.RootCluster.Path(), envtest.WithNamePrefix("something"))
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
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kcp-dev/logicalcluster/v3"
	clusterclient "github.com/kcp-dev/multicluster-provider/client"
	"github.com/kcp-dev/multicluster-provider/envtest"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	// KubeconfigEnv names an existing kcp to attach to, like --kcp-kubeconfig.
	KubeconfigEnv = "KCP_KUBECONFIG"
	// AssetsEnv is the directory holding kcp, kcp-front-proxy, cache-server
	// and sharded-test-server.
	AssetsEnv = "TEST_KCP_ASSETS"

	baseContext     = "base"
	logTailLines    = 50
	portWaitTimeout = time.Minute
	// pidFile in the work dir holds the pid of the package's
	// sharded-test-server (also its process group id).
	pidFile = "server.pid"
)

var kubeconfigFlag = flag.String("kcp-kubeconfig", "", "kubeconfig of an existing kcp to run against instead of starting a sharded-test-server (also "+KubeconfigEnv+"); uses context \"base\" if present, else the current context")

// assets maps the variables sharded-test-server and envtest read to the
// binary names under TEST_KCP_ASSETS.
var assets = []struct{ env, binary string }{
	{"KCP_ASSET_KCP", "kcp"},
	{"KCP_ASSET_KCP_FRONT_PROXY", "kcp-front-proxy"},
	{"KCP_ASSET_CACHE_SERVER", "cache-server"},
	{"KCP_ASSET_SHARDED_TEST_SERVER", "sharded-test-server"},
}

// current is the package's kcp, set by Main.
var current *Env

// Env is a running kcp, private to the test binary or attached to.
type Env struct {
	config     *rest.Config
	shardNames []string
	attached   bool
	// cluster is used for workspace fixtures; its scheme knows the kcp
	// tenancy and core types only.
	cluster clusterclient.ClusterClient
}

// Main is the TestMain of every integration package:
//
//	func TestMain(m *testing.M) { os.Exit(kcp.Main(m)) }
//
// It starts a private sharded-test-server unless --kcp-kubeconfig or
// KCP_KUBECONFIG names an existing kcp, runs the tests and stops the server.
func Main(m *testing.M) int {
	if !flag.Parsed() {
		flag.Parse()
	}
	if os.Getenv("TEST_QUIET") != "1" {
		ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	}
	setDefaultEnv()

	kubeconfig := *kubeconfigFlag
	if kubeconfig == "" {
		kubeconfig = os.Getenv(KubeconfigEnv)
	}

	var (
		cfg  *rest.Config
		stop = func() {}
		err  error
	)
	if kubeconfig != "" {
		cfg, err = loadKubeconfig(kubeconfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kcp: loading %s: %v\n", kubeconfig, err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "kcp: attached to %s (%s)\n", cfg.Host, kubeconfig)
	} else {
		cfg, stop, err = startSharded()
		if err != nil {
			fmt.Fprintf(os.Stderr, "kcp: %v\n", err)
			return 1
		}
	}
	defer stop()

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

// ShardNames returns the names of the kcp shards ("root", "shard-1", ...).
func (e *Env) ShardNames() []string {
	return append([]string(nil), e.shardNames...)
}

// Attached reports whether the kcp was given by --kcp-kubeconfig /
// KCP_KUBECONFIG (and is possibly shared with other test packages).
func (e *Env) Attached() bool {
	return e.attached
}

// Workspace creates a workspace under parent (type root:universal and name
// prefix "e2e-workspace-" unless opts say otherwise) and waits until it is
// Ready and reachable by cluster name and by path. The workspace is deleted
// when t ends unless PRESERVE is set, which Main sets by default.
//
// It does what envtest.NewWorkspaceFixture does, but polls with
// wait.Eventually: envtest's waits run their condition (with require calls)
// in a goroutine that can outlive a failed test.
func (e *Env) Workspace(t testing.TB, parent logicalcluster.Path, opts ...envtest.WorkspaceOption) (*kcptenancyv1alpha1.Workspace, logicalcluster.Path) {
	t.Helper()
	ws := &kcptenancyv1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-workspace-"},
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

// setDefaultEnv points the kcp asset variables at TEST_KCP_ASSETS and sets
// NO_GORUN (run the binaries, not `go run`) and PRESERVE (keep workspace
// fixtures until the server goes away) unless they are set already.
func setDefaultEnv() {
	if dir := os.Getenv(AssetsEnv); dir != "" {
		for _, a := range assets {
			setDefault(a.env, filepath.Join(dir, a.binary))
		}
	}
	setDefault("NO_GORUN", "true")
	setDefault("PRESERVE", "true")
}

func setDefault(key, value string) {
	if _, ok := os.LookupEnv(key); !ok {
		_ = os.Setenv(key, value)
	}
}

// startSharded takes the host-wide test server lock, starts
// sharded-test-server in a fresh work dir and returns its admin config and a
// stop function that stops the server, makes sure no process of it survives
// and releases the lock. SIGINT/SIGTERM stop the server too, so an
// interrupted run does not leave kcp processes behind.
func startSharded() (*rest.Config, func(), error) {
	dir, err := workDir()
	if err != nil {
		return nil, nil, err
	}
	unlock, err := acquireServerLock()
	if err != nil {
		return nil, nil, err
	}
	started := false
	defer func() {
		if !started {
			unlock()
		}
	}()
	if err := freePorts(filepath.Dir(dir) + string(filepath.Separator)); err != nil {
		return nil, nil, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, nil, fmt.Errorf("wiping work dir %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating work dir %s: %w", dir, err)
	}

	fmt.Fprintf(os.Stderr, "kcp: starting sharded-test-server, work dir %s\n", dir)
	server := &envtest.Sharded{WorkDir: dir, StopTimeout: 30 * time.Second}
	startedAt := time.Now()
	if err := startIn(dir, server); err != nil {
		printLogTail(filepath.Join(dir, ".kcp-sharded-test-server", "envtest.log"))
		return nil, nil, fmt.Errorf("starting sharded-test-server (work dir %s): %w", dir, err)
	}
	fmt.Fprintf(os.Stderr, "kcp: ready after %s\n", time.Since(startedAt).Round(100*time.Millisecond))

	// envtest.Sharded starts the server in a process group of its own
	// (pgid = its pid); remember it so Stop can make sure the whole group,
	// kcp shards and cache server included, is gone.
	pgid := 0
	if procs, err := listProcs(); err == nil {
		if p, ok := childServer(procs, os.Getpid()); ok {
			pgid = p.pgid
			if err := os.WriteFile(filepath.Join(dir, pidFile), []byte(strconv.Itoa(p.pid)+"\n"), 0o644); err != nil { //nolint:gosec // not secret
				fmt.Fprintf(os.Stderr, "kcp: writing %s: %v\n", pidFile, err)
			}
		}
	}

	stopServer := func() {
		if err := server.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "kcp: stopping sharded-test-server: %v\n", err)
		}
		if err := killServerGroup(pgid); err != nil {
			fmt.Fprintf(os.Stderr, "kcp: %v\n", err)
		}
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-signals:
			fmt.Fprintf(os.Stderr, "kcp: %s, stopping sharded-test-server\n", sig)
			stopServer()
			unlock()
			os.Exit(1)
		case <-done:
		}
	}()

	stop := func() {
		signal.Stop(signals)
		close(done)
		stopServer()
		unlock()
	}
	started = true
	return server.Config(), stop, nil
}

// freePorts makes sure serverPorts are free before a server starts. The
// caller holds the server lock, so a server of this framework still holding
// them was left behind by a test binary that died (go test's timeout, a
// panic in a goroutine, SIGKILL): such orphans, recognised by a work dir
// under testDir and a dead parent, are killed. Otherwise it waits up to
// portWaitTimeout (the previous lock holder's processes may still be
// shutting down, or a server not started by this framework is running) and
// then fails naming whatever holds the ports.
func freePorts(testDir string) error {
	if len(busyPorts()) > 0 {
		reapOrphanedServers(testDir)
	}
	busy := waitForPorts()
	if len(busy) == 0 {
		return nil
	}
	return fmt.Errorf("ports %v still busy after %s, a kcp test server not started by this framework is probably running:\n%s", busy, portWaitTimeout, portHolders(busy))
}

// startIn starts server with dir as the working directory: sharded-test-server
// writes the cache server's kcp.log to ./.kcp-cache relative to its working
// directory, whatever -work-dir-path says. Main calls this before any test
// runs, so changing the process's directory briefly is safe.
func startIn(dir string, server *envtest.Sharded) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	startErr := server.Start(context.Background())
	if err := os.Chdir(cwd); err != nil {
		return fmt.Errorf("returning to %s: %w", cwd, err)
	}
	return startErr
}

// serverPorts are the fixed ports of sharded-test-server with two shards:
// front-proxy, shards, the shards' embedded etcd (client, peer), cache
// server etcd (client, peer) and cache server.
var serverPorts = []int{6443, 6444, 6445, 2379, 2380, 2381, 2382, 8010, 8011, 8012}

// waitForPorts waits up to portWaitTimeout for serverPorts to be free and
// returns those still busy.
func waitForPorts() []int {
	deadline := time.Now().Add(portWaitTimeout)
	logged := false
	for {
		busy := busyPorts()
		if len(busy) == 0 || time.Now().After(deadline) {
			return busy
		}
		if !logged {
			fmt.Fprintf(os.Stderr, "kcp: waiting for ports %v to be free:\n%s\n", busy, portHolders(busy))
			logged = true
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func busyPorts() []int {
	var busy []int
	for _, port := range serverPorts {
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			busy = append(busy, port)
			continue
		}
		_ = l.Close()
	}
	return busy
}

// workDir is <tests module root>/.test/<package import path, / replaced by _>.
func workDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, module, err := findModule(cwd)
	if err != nil {
		return "", err
	}
	pkg := module
	if rel, err := filepath.Rel(root, cwd); err == nil && rel != "." {
		pkg += "/" + filepath.ToSlash(rel)
	}
	return filepath.Join(root, ".test", strings.ReplaceAll(pkg, "/", "_")), nil
}

// findModule walks up from dir to the closest go.mod and returns its
// directory and module path.
func findModule(dir string) (string, string, error) {
	for d := dir; ; d = filepath.Dir(d) {
		data, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil {
			for line := range strings.Lines(string(data)) {
				if module, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
					return d, strings.Trim(strings.TrimSpace(module), `"`), nil
				}
			}
			return "", "", fmt.Errorf("no module line in %s", filepath.Join(d, "go.mod"))
		}
		if filepath.Dir(d) == d {
			return "", "", fmt.Errorf("no go.mod above %s", dir)
		}
	}
}

func printLogTail(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kcp: reading %s: %v\n", path, err)
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > logTailLines {
		lines = lines[len(lines)-logTailLines:]
	}
	fmt.Fprintf(os.Stderr, "kcp: last %d lines of %s:\n%s\n", len(lines), path, strings.Join(lines, "\n"))
}

// loadKubeconfig loads the "base" context if the kubeconfig has one, else
// the current context. A /clusters/<path> suffix on the server is dropped so
// the config is unscoped like the private server's.
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
