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
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	language "github.com/openfga/language/pkg/go/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	fgatest "go.platform-mesh.io/golang-commons/test"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"

	"k8s.io/apimachinery/pkg/api/meta"
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

func newMigrationEnv(t *testing.T, tuples []string) *migrationEnv {
	t.Helper()
	ctx := context.Background()

	srv, err := fgatest.NewOpenFGATestServer(ctx, fgatest.WithAuthorizationModel("model\n  schema 1.2\n\ntype user\n"))
	require.NoError(t, err)
	client, err := srv.Start()
	require.NoError(t, err)
	t.Cleanup(srv.Stop)

	stores, err := client.ListStores(ctx, &openfgav1.ListStoresRequest{})
	require.NoError(t, err)
	require.Len(t, stores.GetStores(), 1)
	storeID := stores.GetStores()[0].GetId()

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

	t.Run("a bad annotation skips only the rename of its own model", func(t *testing.T) {
		e := newMigrationEnv(t, grantsOnOldTypes)
		e.models[0].Annotations[legacyTypeAnnotation] = "_other_type"

		require.NoError(t, e.process(e.fga))
		assert.Subset(t, e.tuples(), []string{crbNewObj + "#owner@" + crbNewRole + "#assignee", crbNewRole + "#assignee@user:bob"})
		assert.Subset(t, e.tuples(), []string{policyOldRole + "#assignee@user:alice"}, "not moved")
		assert.Contains(t, e.currentTypes(), policyNew)
		condition := meta.FindStatusCondition(e.store.Status.Conditions, legacyTypeAnnotationCondition)
		require.NotNil(t, condition)
		assert.Equal(t, metav1.ConditionFalse, condition.Status)
		assert.Contains(t, condition.Message, `annotation core.platform-mesh.io/legacy-fga-type "_other_type" is not a shortened form`)

		e.models[0].Annotations[legacyTypeAnnotation] = policyOld
		require.NoError(t, e.process(e.fga))
		assert.Nil(t, meta.FindStatusCondition(e.store.Status.Conditions, legacyTypeAnnotationCondition))
		assert.Subset(t, e.tuples(), []string{policyOldRole + "#assignee@user:alice"}, "the old type already left the model")
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

func TestLegacyTypeName(t *testing.T) {
	tests := []struct {
		group, plural, singular string
		want                    string
		ok                      bool
	}{
		// Shortened types found in every org store before #436.
		{"rbac.authorization.k8s.io", "clusterrolebindings", "clusterrolebinding", "ac_authorization_k8s_io_clusterrolebinding", true},
		{"authorization.k8s.io", "selfsubjectaccessreviews", "selfsubjectaccessreview", "thorization_k8s_io_selfsubjectaccessreview", true},
		{"authorization.k8s.io", "selfsubjectrulesreviews", "selfsubjectrulesreview", "uthorization_k8s_io_selfsubjectrulesreview", true},
		{"authorization.k8s.io", "localsubjectaccessreviews", "localsubjectaccessreview", "horization_k8s_io_localsubjectaccessreview", true},
		{"alpha.inventory.platform-mesh.example.io", "widgets", "widget", "_inventory_platform-mesh_example_io_widget", true},
		{"bravo.inventory.platform-mesh.example.io", "widgets", "widget", "_inventory_platform-mesh_example_io_widget", true},
		{"security.a-long-provider-name.example.io", "policies", "policy", "ty_a-long-provider-name_example_io_policy", true},
		{"rbac.authorization.k8s.io", "clusterroles", "clusterrole", "rbac_authorization_k8s_io_clusterrole", true},
		{"", "pods", "pod", "core_pod", true},
		// The previous algorithm panicked on these.
		{"generators.external-secrets.io", "beyondtrustworkloadcredentialsdynamicsecrets", "beyondtrustworkloadcredentialsdynamicsecret", "", false},
		{"example.io", "abcdefghijabcdefghijabcdefghijabcdefghijabcde", "abcdefghijabcdefghijabcdefghijabcdefghijabcd", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.group+"/"+tt.plural, func(t *testing.T) {
			got, ok := legacyTypeName(tt.group, tt.plural, tt.singular)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLegacyRename(t *testing.T) {
	tests := []struct {
		group, plural, singular string
		legacy                  string
	}{
		{"alpha.inventory.platform-mesh.example.io", "widgets", "widget", "_inventory_platform-mesh_example_io_widget"},
		{"bravo.inventory.platform-mesh.example.io", "widgets", "widget", "_inventory_platform-mesh_example_io_widget"},
		{"security.a-long-provider-name.example.io", "policies", "policy", "ty_a-long-provider-name_example_io_policy"},
		{"rbac.authorization.k8s.io", "clusterrolebindings", "clusterrolebinding", "ac_authorization_k8s_io_clusterrolebinding"},
		{"authorization.k8s.io", "selfsubjectaccessreviews", "selfsubjectaccessreview", "thorization_k8s_io_selfsubjectaccessreview"},
		{"authorization.k8s.io", "selfsubjectrulesreviews", "selfsubjectrulesreview", "uthorization_k8s_io_selfsubjectrulesreview"},
		{"authorization.k8s.io", "localsubjectaccessreviews", "localsubjectaccessreview", "horization_k8s_io_localsubjectaccessreview"},
		// No legacy name: the previous algorithm panicked.
		{"generators.external-secrets.io", "beyondtrustworkloadcredentialsdynamicsecrets", "beyondtrustworkloadcredentialsdynamicsecret", ""},
		// Not renamed: the name fit before #436.
		{"", "namespaces", "namespace", ""},
		{"", "configmaps", "configmap", ""},
		{"", "serviceaccounts", "serviceaccount", ""},
		{"rbac.authorization.k8s.io", "clusterroles", "clusterrole", ""},
		{"rbac.authorization.k8s.io", "roles", "role", ""},
		{"rbac.authorization.k8s.io", "rolebindings", "rolebinding", ""},
		{"authentication.k8s.io", "tokenreviews", "tokenreview", ""},
		{"authentication.k8s.io", "selfsubjectreviews", "selfsubjectreview", ""},
		{"authorization.k8s.io", "subjectaccessreviews", "subjectaccessreview", ""},
		{"apis.kcp.io", "apibindings", "apibinding", ""},
		{"apis.kcp.io", "apiexports", "apiexport", ""},
		{"ui.platform-mesh.io", "contentconfigurations", "contentconfiguration", ""},
		{"orchestrate.platform-mesh.io", "httpbins", "httpbin", ""},
	}

	for _, tt := range tests {
		t.Run(tt.group+"/"+tt.plural, func(t *testing.T) {
			legacy, renamed := legacyRename(tt.group, tt.plural, tt.singular)
			assert.Equal(t, tt.legacy, legacy)
			assert.Equal(t, tt.legacy != "", renamed)
		})
	}
}

func TestTypeRenamesFromAuthorizationModel(t *testing.T) {
	in, err := newModelInput("alpha.inventory.platform-mesh.example.io", "widgets", "widget", "Cluster")
	require.NoError(t, err)

	model := pmcorev1alpha1.AuthorizationModel{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "alpha",
			Annotations: map[string]string{legacyTypeAnnotation: "_inventory_platform-mesh_example_io_widget"},
		},
		Spec: pmcorev1alpha1.AuthorizationModelSpec{Model: render(t, modelTpl, in)},
	}

	renames := typeRenames{}
	require.NoError(t, renames.addAuthorizationModel(model))
	assert.Equal(t, typeRenames{"_inventory_platform-mesh_example_io_widget": {"alpha_inventory_platform-mesh_example_io_widget"}}, renames)

	model.Annotations = nil
	renames = typeRenames{}
	require.NoError(t, renames.addAuthorizationModel(model))
	assert.Empty(t, renames)

	// Old names were cut from the front, anything else would redirect the
	// tuples of an unrelated type.
	for _, legacy := range []string{
		"alpha_inventory_platform-mesh_example_io_widget",
		"bravo_inventory_platform-mesh_example_io_widget",
		"rbac_authorization_k8s_io_clusterrolebinding",
		"_inventory_platform-mesh_example_io",
	} {
		model.Annotations = map[string]string{legacyTypeAnnotation: legacy}
		renames = typeRenames{}
		assert.ErrorContains(t, renames.addAuthorizationModel(model), "is not a shortened form of type", legacy)
		assert.Empty(t, renames)
	}
}

func TestRenameKey(t *testing.T) {
	renames := typeRenames{"ac_authorization_k8s_io_clusterrolebinding": {"rbac_authorization_k8s_io_clusterrolebinding"}}

	tests := map[string]string{
		"ac_authorization_k8s_io_clusterrolebinding:c/crb":                     "rbac_authorization_k8s_io_clusterrolebinding:c/crb",
		"role:ac_authorization_k8s_io_clusterrolebinding/c/crb/owner":          "role:rbac_authorization_k8s_io_clusterrolebinding/c/crb/owner",
		"role:ac_authorization_k8s_io_clusterrolebinding/c/crb/owner#assignee": "role:rbac_authorization_k8s_io_clusterrolebinding/c/crb/owner#assignee",
		"ac_authorization_k8s_io_clusterrolebinding:c/crb#owner":               "rbac_authorization_k8s_io_clusterrolebinding:c/crb#owner",
		"rbac_authorization_k8s_io_clusterrolebinding:c/crb":                   "rbac_authorization_k8s_io_clusterrolebinding:c/crb",
		"role:core_platform-mesh_io_account/c/acc/owner#assignee":              "role:core_platform-mesh_io_account/c/acc/owner#assignee",
		"role:authenticated#assignee":                                          "role:authenticated#assignee",
		"user:ac_authorization_k8s_io_clusterrolebinding":                      "user:ac_authorization_k8s_io_clusterrolebinding",
	}
	for in, want := range tests {
		assert.Equal(t, want, renameKey(in, renames), in)
	}
}
