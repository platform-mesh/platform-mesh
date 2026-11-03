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

package subroutine

import (
	"bytes"
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"text/template"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	language "github.com/openfga/language/pkg/go/transformer"
	"github.com/openfga/openfga/pkg/server"
	"github.com/openfga/openfga/pkg/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
	"go.platform-mesh.io/security-operator/pkg/config"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"

	"github.com/kcp-dev/logicalcluster/v3"
)

const (
	migrationCluster = "path"

	policyGroup    = "security.a-long-provider-name.example.io"
	policyOld      = "ty_a-long-provider-name_example_io_policy"
	policyNew      = "security_a-long-provider-name_example_io_policy"
	crbGroup       = "rbac.authorization.k8s.io"
	crbOld         = "ac_authorization_k8s_io_clusterrolebinding"
	crbNew         = "rbac_authorization_k8s_io_clusterrolebinding"
	accountObject  = "core_platform-mesh_io_account:c/acc"
	policyOldObj   = policyOld + ":c/p1"
	policyNewObj   = policyNew + ":c/p1"
	policyOldRole  = "role:" + policyOld + "/c/p1/owner"
	policyNewRole  = "role:" + policyNew + "/c/p1/owner"
	crbOldObj      = crbOld + ":c/crb"
	crbNewObj      = crbNew + ":c/crb"
	crbOldRole     = "role:" + crbOld + "/c/crb/owner"
	crbNewRole     = "role:" + crbNew + "/c/crb/owner"
	accountOwnRole = "role:core_platform-mesh_io_account/c/acc/owner"
)

const migrationCoreModule = `module core

type user

type role
  relations
    define assignee: [user, user:*]

type core_platform-mesh_io_account
  relations
    define owner: [role#assignee]
    define member: [role#assignee] or owner
`

// legacyModelInput renders names the way they were generated before #436.
func legacyModelInput(t *testing.T, group, plural, singular string) modelInput {
	t.Helper()
	typeName, ok := legacyTypeName(group, plural, singular)
	require.True(t, ok)
	g := strings.TrimSuffix(typeName, "_"+singular)
	return modelInput{
		Name:           plural,
		Scope:          "Cluster",
		TypeName:       typeName,
		CreateRelation: "create_" + g + "_" + plural,
		ListRelation:   "list_" + g + "_" + plural,
		WatchRelation:  "watch_" + g + "_" + plural,
	}
}

func render(t *testing.T, tpl *template.Template, in modelInput) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, tpl.Execute(&buf, in))
	return buf.String()
}

// faultyFGA fails selected calls to simulate a crash between migration steps.
type faultyFGA struct {
	openfgav1.OpenFGAServiceClient
	failDeletes     bool
	failModelWrite  int
	modelWrites     int
	tupleWriteCalls int
}

func (f *faultyFGA) Write(ctx context.Context, in *openfgav1.WriteRequest, opts ...grpc.CallOption) (*openfgav1.WriteResponse, error) {
	f.tupleWriteCalls++
	if f.failDeletes && len(in.GetDeletes().GetTupleKeys()) > 0 {
		return nil, errors.New("crash before delete")
	}
	return f.OpenFGAServiceClient.Write(ctx, in, opts...)
}

func (f *faultyFGA) WriteAuthorizationModel(ctx context.Context, in *openfgav1.WriteAuthorizationModelRequest, opts ...grpc.CallOption) (*openfgav1.WriteAuthorizationModelResponse, error) {
	f.modelWrites++
	if f.modelWrites == f.failModelWrite {
		return nil, errors.New("crash before model write")
	}
	return f.OpenFGAServiceClient.WriteAuthorizationModel(ctx, in, opts...)
}

type migrationEnv struct {
	t       *testing.T
	fga     openfgav1.OpenFGAServiceClient
	storeID string
	store   *pmcorev1alpha1.Store
	models  []pmcorev1alpha1.AuthorizationModel
}

// newOpenFGAServer starts an in-memory OpenFGA server with one empty store.
func newOpenFGAServer(t *testing.T) (openfgav1.OpenFGAServiceClient, string) {
	t.Helper()
	srv := server.MustNewServerWithOpts(server.WithDatastore(memory.New()))
	t.Cleanup(srv.Close)

	lis := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	openfgav1.RegisterOpenFGAServiceServer(grpcServer, srv)
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	store, err := srv.CreateStore(context.Background(), &openfgav1.CreateStoreRequest{Name: "test"})
	require.NoError(t, err)
	return openfgav1.NewOpenFGAServiceClient(conn), store.GetId()
}

func newMigrationEnv(t *testing.T, tuples []string) *migrationEnv {
	t.Helper()
	ctx := context.Background()

	client, storeID := newOpenFGAServer(t)

	// The model an operator before #436 wrote for this org.
	old, err := language.TransformModuleFilesToModel([]language.ModuleFile{
		{Name: "core.fga", Contents: migrationCoreModule},
		{Name: "crb.fga", Contents: render(t, privilegedTemplate, legacyModelInput(t, crbGroup, "clusterrolebindings", "clusterrolebinding"))},
		{Name: "policy.fga", Contents: render(t, privilegedTemplate, legacyModelInput(t, policyGroup, "policies", "policy"))},
	}, schemaVersion)
	require.NoError(t, err)
	res, err := client.WriteAuthorizationModel(ctx, &openfgav1.WriteAuthorizationModelRequest{
		StoreId: storeID, TypeDefinitions: old.TypeDefinitions, SchemaVersion: schemaVersion,
	})
	require.NoError(t, err)

	for _, tuple := range tuples {
		_, err := client.Write(ctx, &openfgav1.WriteRequest{
			StoreId: storeID, AuthorizationModelId: res.GetAuthorizationModelId(),
			Writes: &openfgav1.WriteRequestWrites{TupleKeys: []*openfgav1.TupleKey{parseTuple(t, tuple)}},
		})
		require.NoError(t, err, tuple)
	}

	policy, err := newModelInput(policyGroup, "policies", "policy", "Cluster")
	require.NoError(t, err)

	return &migrationEnv{
		t:       t,
		fga:     client,
		storeID: storeID,
		store: &pmcorev1alpha1.Store{
			ObjectMeta: metav1.ObjectMeta{Name: "org"},
			Spec:       pmcorev1alpha1.StoreSpec{CoreModule: migrationCoreModule},
			Status:     pmcorev1alpha1.StoreStatus{StoreID: storeID, AuthorizationModelID: res.GetAuthorizationModelId()},
		},
		models: []pmcorev1alpha1.AuthorizationModel{{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "policies",
				Annotations: map[string]string{legacyTypeAnnotation: policyOld},
			},
			Spec: pmcorev1alpha1.AuthorizationModelSpec{
				Model:    render(t, privilegedTemplate, policy),
				StoreRef: pmcorev1alpha1.WorkspaceStoreRef{Name: "org", Cluster: migrationCluster},
			},
		}},
	}
}

func (e *migrationEnv) process(client openfgav1.OpenFGAServiceClient) error {
	t := e.t
	lister := mocks.NewMockLister(t)
	lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, list ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) error {
			list.(*pmcorev1alpha1.AuthorizationModelList).Items = e.models
			return nil
		})

	dc := mocks.NewMockDiscoveryInterface(t)
	dc.EXPECT().ServerResourcesForGroupVersion(mock.Anything).RunAndReturn(func(gv string) (*metav1.APIResourceList, error) {
		list := &metav1.APIResourceList{GroupVersion: gv}
		if gv == "rbac.authorization.k8s.io/v1" {
			list.APIResources = []metav1.APIResource{{Name: "clusterrolebindings", SingularName: "clusterrolebinding"}}
		}
		return list, nil
	})

	mgr := mocks.NewMockManager(t)
	ctrlManager := mocks.NewMockCTRLManager(t)
	mgr.EXPECT().GetLocalManager().Return(ctrlManager)
	ctrlManager.EXPECT().GetConfig().Return(&rest.Config{})

	sub := NewAuthorizationModelSubroutine(client, mgr, lister, func(*rest.Config) discovery.DiscoveryInterface { return dc }, nil)
	ctx := mccontext.WithCluster(context.Background(), config.StoreClusterName(logicalcluster.Name(migrationCluster).String()))
	_, err := sub.Process(ctx, e.store)
	return err
}

func (e *migrationEnv) tuples() []string {
	var all []string
	var token string
	for {
		res, err := e.fga.Read(context.Background(), &openfgav1.ReadRequest{StoreId: e.storeID, ContinuationToken: token})
		require.NoError(e.t, err)
		for _, tuple := range res.GetTuples() {
			k := tuple.GetKey()
			all = append(all, k.GetObject()+"#"+k.GetRelation()+"@"+k.GetUser())
		}
		if token = res.GetContinuationToken(); token == "" {
			break
		}
	}
	slices.Sort(all)
	return all
}

func (e *migrationEnv) allowed(user, relation, object string) bool {
	res, err := e.fga.Check(context.Background(), &openfgav1.CheckRequest{
		StoreId:              e.storeID,
		AuthorizationModelId: e.store.Status.AuthorizationModelID,
		TupleKey:             &openfgav1.CheckRequestTupleKey{User: user, Relation: relation, Object: object},
	})
	require.NoError(e.t, err)
	return res.GetAllowed()
}

func (e *migrationEnv) currentTypes() []string {
	res, err := e.fga.ReadAuthorizationModel(context.Background(), &openfgav1.ReadAuthorizationModelRequest{
		StoreId: e.storeID, Id: e.store.Status.AuthorizationModelID,
	})
	require.NoError(e.t, err)
	types := make([]string, 0, len(res.GetAuthorizationModel().GetTypeDefinitions()))
	for _, td := range res.GetAuthorizationModel().GetTypeDefinitions() {
		types = append(types, td.GetType())
	}
	return types
}

func (e *migrationEnv) modelCount() int {
	res, err := e.fga.ReadAuthorizationModels(context.Background(), &openfgav1.ReadAuthorizationModelsRequest{StoreId: e.storeID})
	require.NoError(e.t, err)
	return len(res.GetAuthorizationModels())
}

func parseTuple(t *testing.T, s string) *openfgav1.TupleKey {
	t.Helper()
	object, rest, ok := strings.Cut(s, "#")
	require.True(t, ok, s)
	relation, user, ok := strings.Cut(rest, "@")
	require.True(t, ok, s)
	return &openfgav1.TupleKey{Object: object, Relation: relation, User: user}
}

var grantsOnOldTypes = []string{
	accountObject + "#owner@" + accountOwnRole + "#assignee",
	accountOwnRole + "#assignee@user:carol",
	policyOldObj + "#parent@" + accountObject,
	policyOldObj + "#owner@" + policyOldRole + "#assignee",
	policyOldRole + "#assignee@user:alice",
	crbOldObj + "#owner@" + crbOldRole + "#assignee",
	crbOldRole + "#assignee@user:bob",
}

var grantsOnNewTypes = []string{
	accountObject + "#owner@" + accountOwnRole + "#assignee",
	crbNewObj + "#owner@" + crbNewRole + "#assignee",
	crbNewRole + "#assignee@user:bob",
	accountOwnRole + "#assignee@user:carol",
	policyNewRole + "#assignee@user:alice",
	policyNewObj + "#owner@" + policyNewRole + "#assignee",
	policyNewObj + "#parent@" + accountObject,
}

func (e *migrationEnv) assertMigrated() {
	t := e.t
	assert.ElementsMatch(t, grantsOnNewTypes, e.tuples())

	types := e.currentTypes()
	assert.Subset(t, types, []string{policyNew, crbNew})
	assert.NotContains(t, types, policyOld)
	assert.NotContains(t, types, crbOld)

	assert.True(t, e.allowed("user:alice", "owner", policyNewObj))
	assert.True(t, e.allowed("user:alice", "delete", policyNewObj))
	assert.True(t, e.allowed("user:bob", "owner", crbNewObj))
	assert.True(t, e.allowed("user:carol", "get", policyNewObj), "inherited from the account")
	assert.False(t, e.allowed("user:bob", "owner", policyNewObj))
}

func TestMigrateRenamedTypes(t *testing.T) {
	t.Run("grants on shortened types survive", func(t *testing.T) {
		e := newMigrationEnv(t, grantsOnOldTypes)
		require.NoError(t, e.process(e.fga))
		e.assertMigrated()
	})

	t.Run("rerun is a no-op", func(t *testing.T) {
		e := newMigrationEnv(t, grantsOnOldTypes)
		require.NoError(t, e.process(e.fga))
		modelID, models := e.store.Status.AuthorizationModelID, e.modelCount()

		f := &faultyFGA{OpenFGAServiceClient: e.fga}
		require.NoError(t, e.process(f))
		assert.Equal(t, 0, f.modelWrites)
		assert.Equal(t, 0, f.tupleWriteCalls)
		assert.Equal(t, modelID, e.store.Status.AuthorizationModelID)
		assert.Equal(t, models, e.modelCount())
		e.assertMigrated()
	})

	t.Run("crash after copying, before deleting", func(t *testing.T) {
		e := newMigrationEnv(t, grantsOnOldTypes)
		oldModelID := e.store.Status.AuthorizationModelID

		require.ErrorContains(t, e.process(&faultyFGA{OpenFGAServiceClient: e.fga, failDeletes: true}), "crash before delete")
		assert.Equal(t, oldModelID, e.store.Status.AuthorizationModelID)
		assert.Subset(t, e.tuples(), grantsOnOldTypes, "originals kept")
		assert.Subset(t, e.tuples(), grantsOnNewTypes, "copies written")

		require.NoError(t, e.process(e.fga))
		e.assertMigrated()
	})

	t.Run("crash before writing the transition model", func(t *testing.T) {
		e := newMigrationEnv(t, grantsOnOldTypes)
		require.ErrorContains(t, e.process(&faultyFGA{OpenFGAServiceClient: e.fga, failModelWrite: 1}), "crash before model write")
		assert.ElementsMatch(t, grantsOnOldTypes, e.tuples())

		require.NoError(t, e.process(e.fga))
		e.assertMigrated()
	})

	t.Run("crash after moving the tuples, before writing the final model", func(t *testing.T) {
		e := newMigrationEnv(t, grantsOnOldTypes)
		oldModelID := e.store.Status.AuthorizationModelID

		require.ErrorContains(t, e.process(&faultyFGA{OpenFGAServiceClient: e.fga, failModelWrite: 2}), "crash before model write")
		assert.Equal(t, oldModelID, e.store.Status.AuthorizationModelID)
		assert.ElementsMatch(t, grantsOnNewTypes, e.tuples())

		require.NoError(t, e.process(e.fga))
		e.assertMigrated()
	})

	t.Run("old operator already dropped the old provider type", func(t *testing.T) {
		// A new generator ran before the new operator: the model lost the old
		// provider type but still has the old built-in types.
		e := newMigrationEnv(t, grantsOnOldTypes)
		old, err := language.TransformModuleFilesToModel([]language.ModuleFile{
			{Name: "core.fga", Contents: migrationCoreModule},
			{Name: "crb.fga", Contents: render(t, privilegedTemplate, legacyModelInput(t, crbGroup, "clusterrolebindings", "clusterrolebinding"))},
			{Name: "policy.fga", Contents: e.models[0].Spec.Model},
		}, schemaVersion)
		require.NoError(t, err)
		res, err := e.fga.WriteAuthorizationModel(context.Background(), &openfgav1.WriteAuthorizationModelRequest{
			StoreId: e.storeID, TypeDefinitions: old.TypeDefinitions, SchemaVersion: schemaVersion,
		})
		require.NoError(t, err)
		e.store.Status.AuthorizationModelID = res.GetAuthorizationModelId()

		require.NoError(t, e.process(e.fga))
		e.assertMigrated()
	})

	t.Run("nothing to migrate", func(t *testing.T) {
		e := newMigrationEnv(t, []string{
			accountObject + "#owner@" + accountOwnRole + "#assignee",
			accountOwnRole + "#assignee@user:carol",
		})
		models := e.modelCount()

		f := &faultyFGA{OpenFGAServiceClient: e.fga}
		require.NoError(t, e.process(f))
		assert.Equal(t, 1, f.modelWrites, "only the final model")
		assert.Equal(t, 0, f.tupleWriteCalls)
		assert.Equal(t, models+1, e.modelCount())
		assert.NotContains(t, e.currentTypes(), crbOld)
	})

	t.Run("tuples of a type shared by two APIs fail visibly", func(t *testing.T) {
		e := newMigrationEnv(t, grantsOnOldTypes)
		bravo := e.models[0].DeepCopy()
		bravo.Name = "policies-bravo"
		in, err := newModelInput("bravo."+policyGroup, "policies", "policy", "Cluster")
		require.NoError(t, err)
		bravo.Spec.Model = render(t, privilegedTemplate, in)
		e.models = append(e.models, *bravo)

		err = e.process(e.fga)
		require.ErrorContains(t, err, "type "+policyOld+" was shared by")
		assert.ElementsMatch(t, grantsOnOldTypes, e.tuples())
	})

	t.Run("old name of one API is the current type of another", func(t *testing.T) {
		const subGroup, group = "sub.inventory.platform-mesh.example.io", "inventory.platform-mesh.example.io"
		legacy, renamed := legacyRename(subGroup, "toolkits", "toolkit")
		require.True(t, renamed)
		sub, err := newModelInput(subGroup, "toolkits", "toolkit", "Cluster")
		require.NoError(t, err)
		toolkit, err := newModelInput(group, "toolkits", "toolkit", "Cluster")
		require.NoError(t, err)
		require.Equal(t, toolkit.TypeName, legacy)

		e := newMigrationEnv(t, nil)
		ref := pmcorev1alpha1.WorkspaceStoreRef{Name: "org", Cluster: migrationCluster}
		e.models = []pmcorev1alpha1.AuthorizationModel{
			{ObjectMeta: metav1.ObjectMeta{Name: "sub", Annotations: map[string]string{legacyTypeAnnotation: legacy}}, Spec: pmcorev1alpha1.AuthorizationModelSpec{Model: render(t, modelTpl, sub), StoreRef: ref}},
			{ObjectMeta: metav1.ObjectMeta{Name: "toolkits"}, Spec: pmcorev1alpha1.AuthorizationModelSpec{Model: render(t, modelTpl, toolkit), StoreRef: ref}},
		}
		require.NoError(t, e.process(e.fga))

		obj, role := toolkit.TypeName+":c/t1", "role:"+toolkit.TypeName+"/c/t1/owner"
		grants := []string{obj + "#owner@" + role + "#assignee", role + "#assignee@user:alice"}
		for _, tuple := range grants {
			_, err := e.fga.Write(context.Background(), &openfgav1.WriteRequest{
				StoreId: e.storeID,
				Writes:  &openfgav1.WriteRequestWrites{TupleKeys: []*openfgav1.TupleKey{parseTuple(t, tuple)}},
			})
			require.NoError(t, err, tuple)
		}

		require.NoError(t, e.process(e.fga))
		assert.ElementsMatch(t, grants, e.tuples())
		assert.True(t, e.allowed("user:alice", "owner", obj))
	})
}
