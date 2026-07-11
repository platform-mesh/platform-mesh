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

package e2e

import (
	"embed"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	examplev1alpha1 "go.platform-mesh.io/resource-broker/api/example/v1alpha1"
	"go.platform-mesh.io/resource-broker/pkg/broker"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/yaml"

	"github.com/kcp-dev/logicalcluster/v3"
	mcpclient "github.com/kcp-dev/multicluster-provider/client"
	"github.com/kcp-dev/multicluster-provider/envtest"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

//go:embed setup/*.yaml
var setupFixtures embed.FS

const (
	// acceptAPIExportName is the APIExport (and endpoint slice) serving
	// AcceptAPIs from the broker home workspace.
	acceptAPIExportName = "broker.platform-mesh.io"
	// exampleExportName is the APIExport (and endpoint slice) serving the
	// brokered example resources.
	exampleExportName = "example.platform-mesh.io"
)

var (
	kcpConfig  *rest.Config
	kcpClient  mcpclient.ClusterClient
	testScheme *runtime.Scheme
)

func TestMain(m *testing.M) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))
	os.Exit(runTests(m))
}

// runTests starts a shared kcp instance for the test binary, runs the
// tests, and tears kcp down again.
func runTests(m *testing.M) int {
	env := &envtest.Environment{
		AttachKcpOutput:       false,
		KcpStartTimeout:       2 * time.Minute,
		KcpStopTimeout:        30 * time.Second,
		BinaryAssetsDirectory: "../../../../bin", // TEST_KCP_ASSETS overrides
	}

	if os.Getenv("USE_EXISTING_KCP") != "" && os.Getenv("EXISTING_KCP_CONTEXT") == "" {
		env.ExistingKcpContext = "base"
	}

	// Prevents kcp from cleaning up workspace fixtures mid-run, the
	// instance controlled by envtest is ephemeral anyway.
	if os.Getenv("PRESERVE") == "" {
		if err := os.Setenv("PRESERVE", "true"); err != nil {
			fmt.Fprintf(os.Stderr, "setting PRESERVE: %v\n", err)
			return 1
		}
	}

	var err error
	kcpConfig, err = env.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "starting kcp: %v\n", err)
		return 1
	}
	defer func() {
		if err := env.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "stopping kcp: %v\n", err)
		}
	}()

	testScheme = newTestScheme()
	kcpClient, err = mcpclient.New(kcpConfig, ctrlruntimeclient.Options{Scheme: testScheme})
	if err != nil {
		fmt.Fprintf(os.Stderr, "building kcp client: %v\n", err)
		return 1
	}

	return m.Run()
}

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		apiextensionsv1.AddToScheme,
		kcptenancyv1alpha1.AddToScheme,
		kcpcorev1alpha1.AddToScheme,
		kcpapisv1alpha1.AddToScheme,
		kcpapisv1alpha2.AddToScheme,
		pmbrokerv1alpha1.AddToScheme,
		pmcoordbrokerv1alpha1.AddToScheme,
		examplev1alpha1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			panic(err)
		}
	}
	return scheme
}

// Frame is a broker test fixture: a broker home workspace serving the
// AcceptAPI and brokered exports, a coordination workspace with the
// coordbroker CRDs, and named consumer and provider workspaces.
type Frame struct {
	// HomePath is the broker home workspace. It hosts the AcceptAPI and
	// brokered APIExports and doubles as verification and staging tree root.
	HomePath   logicalcluster.Path
	HomeClient ctrlruntimeclient.Client

	// CoordinationPath is the workspace holding Assignments,
	// StagingWorkspaces and Migrations.
	CoordinationPath   logicalcluster.Path
	CoordinationClient ctrlruntimeclient.Client

	// ComputePath is the workspace migrations deploy stage templates into.
	ComputePath   logicalcluster.Path
	ComputeClient ctrlruntimeclient.Client

	Consumers map[string]*ControlPlane
	Providers map[string]*ControlPlane
}

func NewFrame(tb testing.TB) *Frame {
	tb.Helper()

	f := &Frame{
		Consumers: make(map[string]*ControlPlane),
		Providers: make(map[string]*ControlPlane),
	}

	_, f.HomePath = envtest.NewWorkspaceFixture(tb, kcpClient, core.RootCluster.Path(), envtest.WithNamePrefix("broker-home"))
	f.HomeClient = kcpClient.Cluster(f.HomePath)

	applySchemas(tb, f.HomeClient,
		"apiresourceschema-acceptapis.broker.platform-mesh.io.yaml",
		"apiresourceschema-certificates.example.platform-mesh.io.yaml",
		"apiresourceschema-dnszones.example.platform-mesh.io.yaml",
		"apiresourceschema-vms.example.platform-mesh.io.yaml",
	)
	createExport(tb, f.HomeClient, "apiexport-broker.platform-mesh.io.yaml")
	// The broker writes related resources into consumer workspaces through
	// the brokered virtual workspace; claim configmaps for the tests.
	createExport(tb, f.HomeClient, "apiexport-example.platform-mesh.io.yaml", configMapsClaim())

	_, f.CoordinationPath = envtest.NewWorkspaceFixture(tb, kcpClient, core.RootCluster.Path(), envtest.WithNamePrefix("coordination"))
	f.CoordinationClient = kcpClient.Cluster(f.CoordinationPath)
	applyCRDs(tb, f.CoordinationClient,
		"../../config/coordbroker/crd/coord.broker.platform-mesh.io_assignments.yaml",
		"../../config/coordbroker/crd/coord.broker.platform-mesh.io_migrationconfigurations.yaml",
		"../../config/coordbroker/crd/coord.broker.platform-mesh.io_migrations.yaml",
		"../../config/coordbroker/crd/coord.broker.platform-mesh.io_stagingworkspaces.yaml",
	)

	_, f.ComputePath = envtest.NewWorkspaceFixture(tb, kcpClient, core.RootCluster.Path(), envtest.WithNamePrefix("compute"))
	f.ComputeClient = kcpClient.Cluster(f.ComputePath)

	return f
}

// NewConsumer creates a consumer workspace bound to the broker's brokered export.
func (f *Frame) NewConsumer(tb testing.TB, name string) *ControlPlane {
	tb.Helper()
	require.NotContains(tb, f.Consumers, name, "consumer already exists")

	ws, path := envtest.NewWorkspaceFixture(tb, kcpClient, core.RootCluster.Path(), envtest.WithNamePrefix(name))
	cp := &ControlPlane{Path: path, ClusterName: ws.Spec.Cluster, Client: kcpClient.Cluster(path)}

	createBinding(tb, cp.Client, exampleExportName, f.HomePath.String(), acceptClaims(configMapsClaim()))
	// Endpoint slices only get endpoints once a binding exists.
	waitEndpointSlice(tb, f.HomeClient, exampleExportName)

	f.Consumers[name] = cp
	return cp
}

// NewProvider creates a provider workspace bound to the broker's AcceptAPI
// export and serving its own export of the example resources.
func (f *Frame) NewProvider(tb testing.TB, name string) *ControlPlane {
	tb.Helper()
	require.NotContains(tb, f.Providers, name, "provider already exists")

	ws, path := envtest.NewWorkspaceFixture(tb, kcpClient, core.RootCluster.Path(), envtest.WithNamePrefix(name))
	cp := &ControlPlane{Path: path, ClusterName: ws.Spec.Cluster, Client: kcpClient.Cluster(path)}

	createBinding(tb, cp.Client, acceptAPIExportName, f.HomePath.String(), nil)
	// Endpoint slices only get endpoints once a binding exists.
	waitEndpointSlice(tb, f.HomeClient, acceptAPIExportName)
	applySchemas(tb, cp.Client,
		"apiresourceschema-certificates.example.platform-mesh.io.yaml",
		"apiresourceschema-dnszones.example.platform-mesh.io.yaml",
		"apiresourceschema-vms.example.platform-mesh.io.yaml",
	)
	createExport(tb, cp.Client, "apiexport-example.platform-mesh.io.yaml")

	f.Providers[name] = cp
	return cp
}

// StagingClient waits for the staging workspace serving the given provider
// and returns a client for it.
func (f *Frame) StagingClient(tb testing.TB, provider *ControlPlane) ctrlruntimeclient.Client {
	tb.Helper()

	return kcpClient.Cluster(f.stagingPath(tb, provider))
}

// StagingWatchClient waits for the staging workspace serving the given
// provider and returns a watch-capable client for it.
func (f *Frame) StagingWatchClient(tb testing.TB, provider *ControlPlane) ctrlruntimeclient.WithWatch {
	tb.Helper()

	cfg := rest.CopyConfig(kcpConfig)
	cfg.Host += "/clusters/" + f.stagingPath(tb, provider).String()

	cl, err := ctrlruntimeclient.NewWithWatch(cfg, ctrlruntimeclient.Options{Scheme: testScheme})
	require.NoError(tb, err)
	return cl
}

// stagingPath waits for the staging workspace serving the given provider and
// returns its workspace path.
func (f *Frame) stagingPath(tb testing.TB, provider *ControlPlane) logicalcluster.Path {
	tb.Helper()

	var swName string
	envtest.Eventually(tb, func() (bool, string) {
		list := &pmcoordbrokerv1alpha1.StagingWorkspaceList{}
		if err := f.CoordinationClient.List(tb.Context(), list); err != nil {
			return false, fmt.Sprintf("listing staging workspaces: %v", err)
		}
		for _, sw := range list.Items {
			if sw.Spec.ProviderCluster == provider.ClusterName {
				swName = sw.Name
				return true, ""
			}
		}
		return false, fmt.Sprintf("no staging workspace for provider cluster %s among %d items", provider.ClusterName, len(list.Items))
	}, wait.ForeverTestTimeout, time.Second, "staging workspace for provider cluster %s", provider.ClusterName)

	return f.HomePath.Join(swName)
}

// Options returns broker options wired to this frame's workspaces.
func (f *Frame) Options(tb testing.TB) broker.Options {
	tb.Helper()

	homeConfig := rest.CopyConfig(kcpConfig)
	homeConfig.Host += "/clusters/" + f.HomePath.String()

	computeConfig := rest.CopyConfig(kcpConfig)
	computeConfig.Host += "/clusters/" + f.ComputePath.String()

	return broker.Options{
		Log:                   log.Log.WithName(tb.Name()),
		LocalConfig:           homeConfig,
		KcpConfig:             homeConfig,
		ComputeConfig:         computeConfig,
		AcceptAPIName:         acceptAPIExportName,
		CoordinationWorkspace: f.CoordinationPath.String(),
		VerificationTreeRoot:  f.HomePath.String(),
		StagingTreeRoot:       f.HomePath.String(),
		// Tests run multiple brokers in one process.
		SkipNameValidation: ptr.To(true),
		// Keep the pipeline latency well below test timeouts.
		RequeueInterval: time.Second,
	}
}

// StartBroker runs a broker for the frame until the test ends and fails
// the test if it exits with an error.
func (f *Frame) StartBroker(t *testing.T) {
	t.Helper()

	mgr, err := broker.New(f.Options(t))
	require.NoError(t, err)

	g, ctx := errgroup.WithContext(t.Context())
	g.Go(func() error {
		return mgr.Start(ctx)
	})
	t.Cleanup(func() {
		require.NoError(t, g.Wait())
	})
}

// ControlPlane is a consumer or provider workspace.
type ControlPlane struct {
	Path        logicalcluster.Path
	ClusterName string
	Client      ctrlruntimeclient.Client
}

// fixture returns the contents of an embedded setup fixture.
func fixture(tb testing.TB, name string) []byte {
	tb.Helper()
	data, err := setupFixtures.ReadFile("setup/" + name)
	require.NoError(tb, err)
	return data
}

// applySchemas creates the APIResourceSchemas from the given fixtures.
func applySchemas(tb testing.TB, cl ctrlruntimeclient.Client, fixtures ...string) {
	tb.Helper()
	for _, name := range fixtures {
		schema := &kcpapisv1alpha1.APIResourceSchema{}
		require.NoError(tb, yaml.Unmarshal(fixture(tb, name), schema))
		err := cl.Create(tb.Context(), schema)
		if !apierrors.IsAlreadyExists(err) {
			require.NoError(tb, err)
		}
	}
}

// createExport creates the APIExport from the given fixture, adding the
// given permission claims.
func createExport(tb testing.TB, cl ctrlruntimeclient.Client, fixtureName string, claims ...kcpapisv1alpha2.PermissionClaim) {
	tb.Helper()
	export := &kcpapisv1alpha2.APIExport{}
	require.NoError(tb, yaml.Unmarshal(fixture(tb, fixtureName), export))
	export.Spec.PermissionClaims = append(export.Spec.PermissionClaims, claims...)
	err := cl.Create(tb.Context(), export)
	if !apierrors.IsAlreadyExists(err) {
		require.NoError(tb, err)
	}
}

// waitEndpointSlice waits until the endpoint slice kcp creates for the
// APIExport of the same name has a populated URL.
func waitEndpointSlice(tb testing.TB, cl ctrlruntimeclient.Client, name string) {
	tb.Helper()
	envtest.Eventually(tb, func() (bool, string) {
		slice := &kcpapisv1alpha1.APIExportEndpointSlice{}
		if err := cl.Get(tb.Context(), ctrlruntimeclient.ObjectKey{Name: name}, slice); err != nil {
			return false, fmt.Sprintf("getting endpoint slice: %v", err)
		}
		if len(slice.Status.APIExportEndpoints) == 0 || slice.Status.APIExportEndpoints[0].URL == "" {
			return false, fmt.Sprintf("no endpoint URLs:\n%s", toYAML(tb, slice.Status))
		}
		return true, ""
	}, wait.ForeverTestTimeout, 200*time.Millisecond, "endpoint slice %s should have endpoints", name)
}

// applyCRDs creates the CRDs from the given files and waits until they are
// served.
func applyCRDs(tb testing.TB, cl ctrlruntimeclient.Client, files ...string) {
	tb.Helper()
	for _, file := range files {
		data, err := os.ReadFile(file)
		require.NoError(tb, err)
		crd := &apiextensionsv1.CustomResourceDefinition{}
		require.NoError(tb, yaml.Unmarshal(data, crd))
		err = cl.Create(tb.Context(), crd)
		if !apierrors.IsAlreadyExists(err) {
			require.NoError(tb, err)
		}
		envtest.Eventually(tb, func() (bool, string) {
			current := &apiextensionsv1.CustomResourceDefinition{}
			if err := cl.Get(tb.Context(), ctrlruntimeclient.ObjectKey{Name: crd.Name}, current); err != nil {
				return false, fmt.Sprintf("getting CRD: %v", err)
			}
			for _, cond := range current.Status.Conditions {
				if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
					return true, ""
				}
			}
			return false, fmt.Sprintf("not established:\n%s", toYAML(tb, current.Status.Conditions))
		}, wait.ForeverTestTimeout, 200*time.Millisecond, "CRD %s should be established", crd.Name)
	}
}

// createBinding creates an APIBinding to the given export and waits for it
// to be bound.
func createBinding(tb testing.TB, cl ctrlruntimeclient.Client, exportName, exportPath string, claims []kcpapisv1alpha2.AcceptablePermissionClaim) {
	tb.Helper()
	binding := &kcpapisv1alpha2.APIBinding{}
	binding.Name = exportName
	binding.Spec.Reference.Export = &kcpapisv1alpha2.ExportBindingReference{
		Path: exportPath,
		Name: exportName,
	}
	binding.Spec.PermissionClaims = claims
	err := cl.Create(tb.Context(), binding)
	if !apierrors.IsAlreadyExists(err) {
		require.NoError(tb, err)
	}
	envtest.Eventually(tb, func() (bool, string) {
		current := &kcpapisv1alpha2.APIBinding{}
		if err := cl.Get(tb.Context(), ctrlruntimeclient.ObjectKey{Name: exportName}, current); err != nil {
			return false, fmt.Sprintf("getting APIBinding: %v", err)
		}
		if current.Status.Phase != kcpapisv1alpha2.APIBindingPhaseBound {
			return false, fmt.Sprintf("phase %s:\n%s", current.Status.Phase, toYAML(tb, current.Status.Conditions))
		}
		return true, ""
	}, wait.ForeverTestTimeout, 200*time.Millisecond, "APIBinding %s should be bound", exportName)
}

// toYAML renders x for failure messages.
func toYAML(tb testing.TB, x any) string {
	tb.Helper()
	data, err := yaml.Marshal(x)
	require.NoError(tb, err)
	return string(data)
}

// configMapsClaim claims configmaps for an APIExport.
func configMapsClaim() kcpapisv1alpha2.PermissionClaim {
	return kcpapisv1alpha2.PermissionClaim{
		GroupResource: kcpapisv1alpha2.GroupResource{Group: "", Resource: "configmaps"},
		Verbs:         []string{"*"},
	}
}

// acceptClaims wraps permission claims for acceptance on an APIBinding.
func acceptClaims(claims ...kcpapisv1alpha2.PermissionClaim) []kcpapisv1alpha2.AcceptablePermissionClaim {
	accepted := make([]kcpapisv1alpha2.AcceptablePermissionClaim, 0, len(claims))
	for _, claim := range claims {
		accepted = append(accepted, kcpapisv1alpha2.AcceptablePermissionClaim{
			ScopedPermissionClaim: kcpapisv1alpha2.ScopedPermissionClaim{
				PermissionClaim: claim,
				Selector:        kcpapisv1alpha2.PermissionClaimSelector{MatchAll: true},
			},
			State: kcpapisv1alpha2.ClaimAccepted,
		})
	}
	return accepted
}
