//go:build integration

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

package resourcebroker

import (
	"embed"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/fixtures"
	"go.platform-mesh.io/platform-mesh/tests/framework/kcp"
	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"
	frameworkwait "go.platform-mesh.io/platform-mesh/tests/framework/wait"
	examplev1alpha1 "go.platform-mesh.io/resource-broker/api/example/v1alpha1"
	coordbrokercrds "go.platform-mesh.io/resource-broker/config/coordbroker/crd"
	"go.platform-mesh.io/resource-broker/pkg/broker"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	kcptc "github.com/kcp-dev/kcp-testcontainers"
	"github.com/kcp-dev/logicalcluster/v3"
	clusterclient "github.com/kcp-dev/multicluster-provider/client"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/sdk/apis/core"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// setupFixtures are the kcp APIResourceSchemas and APIExports generated from
// the broker's CRDs by `task -d operators/resource-broker e2e-fixtures`.
//
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

// coordBrokerCRDs are the CRDs the coordination workspace serves.
var coordBrokerCRDs = []string{
	"assignments.coord.broker.platform-mesh.io",
	"migrationconfigurations.coord.broker.platform-mesh.io",
	"migrations.coord.broker.platform-mesh.io",
	"stagingworkspaces.coord.broker.platform-mesh.io",
}

func newTestScheme(tb testing.TB) *runtime.Scheme {
	tb.Helper()
	return scheme.New(tb,
		pmbrokerv1alpha1.AddToScheme,
		pmcoordbrokerv1alpha1.AddToScheme,
		examplev1alpha1.AddToScheme,
	)
}

// Frame is a broker test fixture: a broker home workspace serving the
// AcceptAPI and brokered exports, a coordination workspace with the
// coordbroker CRDs, and named consumer and provider workspaces.
type Frame struct {
	Env     *kcptc.Instance
	Scheme  *runtime.Scheme
	Cluster clusterclient.ClusterClient

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

	env, err := kcp.Default(tb.Context())
	require.NoError(tb, err)
	s := newTestScheme(tb)
	cluster, err := clusterclient.New(env.BaseConfig(), ctrlruntimeclient.Options{Scheme: s})
	require.NoError(tb, err)
	f := &Frame{
		Env:       env,
		Scheme:    s,
		Cluster:   cluster,
		Consumers: make(map[string]*ControlPlane),
		Providers: make(map[string]*ControlPlane),
	}

	_, f.HomePath = f.Workspace(tb, "broker-home")
	f.HomeClient = f.Cluster.Cluster(f.HomePath)

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

	_, f.CoordinationPath = f.Workspace(tb, "coordination")
	f.CoordinationClient = f.Cluster.Cluster(f.CoordinationPath)
	fixtures.ApplyFS(tb, f.CoordinationClient, coordbrokercrds.Resources, "*.yaml")
	for _, name := range coordBrokerCRDs {
		frameworkwait.CRDEstablished(tb, f.CoordinationClient, name)
	}

	_, f.ComputePath = f.Workspace(tb, "compute")
	f.ComputeClient = f.Cluster.Cluster(f.ComputePath)

	return f
}

// Workspace creates a workspace under root with the given name prefix. The
// sharded kcp schedules it on any shard.
func (f *Frame) Workspace(tb testing.TB, prefix string) (*kcptenancyv1alpha1.Workspace, logicalcluster.Path) {
	tb.Helper()
	path, err := f.Env.CreateWorkspace(tb.Context(), core.RootCluster.Path().Join(prefix+"-").String(), kcptc.WaitForReachable())
	require.NoError(tb, err)
	ws := &kcptenancyv1alpha1.Workspace{}
	require.NoError(tb, f.Cluster.Cluster(core.RootCluster.Path()).Get(tb.Context(), ctrlruntimeclient.ObjectKey{Name: path.Base()}, ws))
	return ws, path
}

// NewConsumer creates a consumer workspace bound to the broker's brokered export.
func (f *Frame) NewConsumer(tb testing.TB, name string) *ControlPlane {
	tb.Helper()
	require.NotContains(tb, f.Consumers, name, "consumer already exists")

	ws, path := f.Workspace(tb, name)
	cp := &ControlPlane{Path: path, ClusterName: ws.Spec.Cluster, Client: f.Cluster.Cluster(path)}

	createBinding(tb, cp.Client, exampleExportName, f.HomePath.String(), acceptClaims(configMapsClaim()))
	// Endpoint slices only get endpoints once a binding exists.
	frameworkwait.EndpointSliceServed(tb, f.HomeClient, exampleExportName)

	f.Consumers[name] = cp
	return cp
}

// NewProvider creates a provider workspace bound to the broker's AcceptAPI
// export and serving its own export of the example resources.
func (f *Frame) NewProvider(tb testing.TB, name string, claims ...kcpapisv1alpha2.PermissionClaim) *ControlPlane {
	tb.Helper()
	require.NotContains(tb, f.Providers, name, "provider already exists")

	ws, path := f.Workspace(tb, name)
	cp := &ControlPlane{Path: path, ClusterName: ws.Spec.Cluster, Client: f.Cluster.Cluster(path)}

	createBinding(tb, cp.Client, acceptAPIExportName, f.HomePath.String(), nil)
	// Endpoint slices only get endpoints once a binding exists.
	frameworkwait.EndpointSliceServed(tb, f.HomeClient, acceptAPIExportName)
	applySchemas(tb, cp.Client,
		"apiresourceschema-certificates.example.platform-mesh.io.yaml",
		"apiresourceschema-dnszones.example.platform-mesh.io.yaml",
		"apiresourceschema-vms.example.platform-mesh.io.yaml",
	)
	createExport(tb, cp.Client, "apiexport-example.platform-mesh.io.yaml", claims...)

	f.Providers[name] = cp
	return cp
}

// StagingClient waits for the staging workspace serving the given provider
// and returns a client for it.
func (f *Frame) StagingClient(tb testing.TB, provider *ControlPlane) ctrlruntimeclient.Client {
	tb.Helper()

	return f.Cluster.Cluster(f.stagingPath(tb, provider))
}

// StagingWatchClient waits for the staging workspace serving the given
// provider and returns a watch-capable client for it.
func (f *Frame) StagingWatchClient(tb testing.TB, provider *ControlPlane) ctrlruntimeclient.WithWatch {
	tb.Helper()

	cl, err := ctrlruntimeclient.NewWithWatch(f.Env.RESTConfig(f.stagingPath(tb, provider).String()), ctrlruntimeclient.Options{Scheme: f.Scheme})
	require.NoError(tb, err)
	return cl
}

// stagingPath waits for the staging workspace serving the given provider and
// returns its workspace path.
func (f *Frame) stagingPath(tb testing.TB, provider *ControlPlane) logicalcluster.Path {
	tb.Helper()

	var swName string
	frameworkwait.Eventually(tb, func() (bool, string) {
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

	homeConfig := f.Env.RESTConfig(f.HomePath.String())

	return broker.Options{
		Log:                   log.Log.WithName(tb.Name()),
		LocalConfig:           homeConfig,
		KcpConfig:             homeConfig,
		ComputeConfig:         f.Env.RESTConfig(f.ComputePath.String()),
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
	f.StartBrokerWithOptions(t, f.Options(t))
}

func (f *Frame) StartBrokerWithOptions(t *testing.T, opts broker.Options) {
	t.Helper()

	mgr, err := broker.New(opts)
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

// createBinding creates an APIBinding to the given export and waits for it
// to be bound and its resources to be served.
func createBinding(tb testing.TB, cl ctrlruntimeclient.Client, exportName, exportPath string, claims []kcpapisv1alpha2.AcceptablePermissionClaim) {
	tb.Helper()
	binding := &kcpapisv1alpha2.APIBinding{}
	binding.Name = exportName
	binding.Spec.Reference.Export = &kcpapisv1alpha2.ExportBindingReference{
		Path: exportPath,
		Name: exportName,
	}
	binding.Spec.PermissionClaims = claims
	createAcrossShards(tb, cl, binding)
	frameworkwait.APIBindingBound(tb, cl, exportName)

	// The bound resources appear in discovery a moment after the binding is
	// Bound; wait until the REST mapper resolves every one of them.
	current := &kcpapisv1alpha2.APIBinding{}
	frameworkwait.Eventually(tb, func() (bool, string) {
		if err := cl.Get(tb.Context(), ctrlruntimeclient.ObjectKey{Name: exportName}, current); err != nil {
			return false, fmt.Sprintf("getting APIBinding: %v", err)
		}
		for _, res := range current.Status.BoundResources {
			gvr := schema.GroupVersionResource{Group: res.Group, Resource: res.Resource}
			if _, err := cl.RESTMapper().KindFor(gvr); err != nil {
				return false, fmt.Sprintf("resolving %s: %v", gvr, err)
			}
		}
		return true, ""
	}, frameworkwait.Timeout, frameworkwait.Tick, "APIBinding %s resources should be served", exportName)
}

// createAcrossShards creates obj, tolerating AlreadyExists. Creating an
// object that references an APIExport (APIBinding, APIExportEndpointSlice)
// checks the bind permission in the export's workspace; when that workspace
// is on another shard the RBAC reaches this shard through the cache server a
// moment later, so Forbidden is retried for a while.
func createAcrossShards(tb testing.TB, cl ctrlruntimeclient.Client, obj ctrlruntimeclient.Object) {
	tb.Helper()
	frameworkwait.Eventually(tb, func() (bool, string) {
		err := cl.Create(tb.Context(), obj.DeepCopyObject().(ctrlruntimeclient.Object))
		switch {
		case err == nil, apierrors.IsAlreadyExists(err):
			return true, ""
		case apierrors.IsForbidden(err):
			return false, err.Error()
		default:
			require.NoError(tb, err)
			return false, ""
		}
	}, frameworkwait.Timeout, frameworkwait.Tick, "creating %T %s", obj, obj.GetName())
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

// createAcceptAPI creates an AcceptAPI for the given example resource with the given filters in the provider's workspace.
func createAcceptAPI(t *testing.T, provider *ControlPlane, name, resource string, filters ...pmbrokerv1alpha1.Filter) {
	t.Helper()

	acceptAPI := &pmbrokerv1alpha1.AcceptAPI{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: pmbrokerv1alpha1.AcceptAPISpec{
			GVR: metav1.GroupVersionResource{
				Group:    "example.platform-mesh.io",
				Version:  "v1alpha1",
				Resource: resource,
			},
			APIExportName: exampleExportName,
			Filters:       filters,
		},
	}
	require.NoError(t, provider.Client.Create(t.Context(), acceptAPI))
}

// verificationWorkspaces lists the verification workspaces.
func (f *Frame) verificationWorkspaces(tb testing.TB) []kcptenancyv1alpha1.Workspace {
	tb.Helper()

	list := &kcptenancyv1alpha1.WorkspaceList{}
	if err := f.HomeClient.List(tb.Context(), list); err != nil {
		tb.Logf("listing workspaces: %v", err)
		return nil
	}
	var out []kcptenancyv1alpha1.Workspace
	for _, ws := range list.Items {
		if strings.HasPrefix(ws.Name, "verify-") {
			out = append(out, ws)
		}
	}
	return out
}

// listAssignments lists the Assignments in the coordination workspace.
func (f *Frame) listAssignments(tb testing.TB) []pmcoordbrokerv1alpha1.Assignment {
	tb.Helper()

	list := &pmcoordbrokerv1alpha1.AssignmentList{}
	if err := f.CoordinationClient.List(tb.Context(), list); err != nil {
		tb.Logf("listing assignments: %v", err)
		return nil
	}
	return list.Items
}

// listStagingWorkspaces lists the StagingWorkspaces in the coordination workspace.
func (f *Frame) listStagingWorkspaces(tb testing.TB) []pmcoordbrokerv1alpha1.StagingWorkspace {
	tb.Helper()

	list := &pmcoordbrokerv1alpha1.StagingWorkspaceList{}
	if err := f.CoordinationClient.List(tb.Context(), list); err != nil {
		tb.Logf("listing staging workspaces: %v", err)
		return nil
	}
	return list.Items
}

// listMigrations lists the Migrations in the coordination workspace.
func (f *Frame) listMigrations(tb testing.TB) []pmcoordbrokerv1alpha1.Migration {
	tb.Helper()

	list := &pmcoordbrokerv1alpha1.MigrationList{}
	if err := f.CoordinationClient.List(tb.Context(), list); err != nil {
		tb.Logf("listing migrations: %v", err)
		return nil
	}
	return list.Items
}

// waitForBoundAssignment waits until exactly one Assignment exists and is bound, and returns it.
func waitForBoundAssignment(t *testing.T, frame *Frame) *pmcoordbrokerv1alpha1.Assignment {
	t.Helper()

	assignment := &pmcoordbrokerv1alpha1.Assignment{}
	frameworkwait.Until(t, func() bool {
		assignments := frame.listAssignments(t)
		if len(assignments) != 1 {
			t.Logf("want exactly one assignment, have %d", len(assignments))
			return false
		}
		if assignments[0].Status.Phase != pmcoordbrokerv1alpha1.AssignmentPhaseBound {
			t.Logf("assignment phase is %q", assignments[0].Status.Phase)
			return false
		}
		*assignment = assignments[0]
		return true
	}, wait.ForeverTestTimeout, time.Second, "condition not met")
	return assignment
}

// updateVM gets, mutates, and updates the VM until the update succeeds.
func updateVM(t *testing.T, cl ctrlruntimeclient.Client, nn types.NamespacedName, mutate func(*examplev1alpha1.VM)) {
	t.Helper()

	frameworkwait.Until(t, func() bool {
		current := &examplev1alpha1.VM{}
		if err := cl.Get(t.Context(), nn, current); err != nil {
			t.Logf("getting vm: %v", err)
			return false
		}
		mutate(current)
		if err := cl.Update(t.Context(), current); err != nil {
			t.Logf("updating vm: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second, "condition not met")
}
