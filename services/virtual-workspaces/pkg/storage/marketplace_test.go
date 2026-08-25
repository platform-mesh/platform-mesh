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

package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/virtual-workspaces/pkg/config"

	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/virtual-workspace-framework/pkg/forwardingregistry"
)

// Default provider and cluster values.
const (
	testOrgClusterID      = "acme-org-cluster-id"
	testProviderClusterID = "abcd-provider-cluster-id"
	testProviderName      = "abcd"
	testExportName        = "widgets.abcd.io"
)

func marketplaceTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	utilruntime.Must(pmuiv1alpha1.AddToScheme(s))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(s))
	utilruntime.Must(kcpapisv1alpha2.AddToScheme(s))
	return s
}

func makeProviderMeta(clusterID string) *pmuiv1alpha1.ProviderMetadata {
	return &pmuiv1alpha1.ProviderMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name: testProviderName,
			Annotations: map[string]string{
				"kcp.io/cluster": clusterID,
			},
		},
	}
}

// makeDefaultExport with default values.
func makeDefaultExport(cfg config.ServiceConfig) *kcpapisv1alpha2.APIExport {
	return makeExport(cfg, testExportName, testProviderClusterID, testProviderName)
}

// makeExport returns an APIExport with the given name and provider label.
func makeExport(cfg config.ServiceConfig, name, clusterID, provider string) *kcpapisv1alpha2.APIExport {
	return &kcpapisv1alpha2.APIExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{"kcp.io/cluster": clusterID},
			Labels:      map[string]string{cfg.ContentForLabel: provider},
		},
		Spec: kcpapisv1alpha2.APIExportSpec{
			Resources: []kcpapisv1alpha2.ResourceSchema{{
				Group:  "example.io",
				Name:   "widgets",
				Schema: "v260810" + name,
				Storage: kcpapisv1alpha2.ResourceSchemaStorage{
					CRD: &kcpapisv1alpha2.ResourceSchemaStorageCRD{},
				},
			}},
		},
	}
}

// listMarketplace decorates and runs the Marketplace func.
func listMarketplace(
	t *testing.T,
	lister ctrlruntimeclient.Client,
	bindings ctrlruntimeclient.Client,
	cfg config.ServiceConfig,
) *unstructured.UnstructuredList {
	t.Helper()

	getClusterClient := func(context.Context, string) (ctrlruntimeclient.Client, error) {
		return bindings, nil
	}

	var funcs forwardingregistry.StoreFuncs
	Marketplace(lister, getClusterClient, cfg).
		Decorate(schema.GroupResource{}, &funcs)

	ctx := genericapirequest.WithCluster(t.Context(), genericapirequest.Cluster{
		Name: logicalcluster.Name(testOrgClusterID),
	})

	obj, err := funcs.ListerFunc(ctx, &internalversion.ListOptions{})
	require.NoError(t, err)

	list, ok := obj.(*unstructured.UnstructuredList)
	require.True(t, ok, "lister returned %T, want *unstructured.UnstructuredList", obj)

	return list
}

func TestMarketplace_ExportsFromForeignCluster(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)

	const (
		providerClusterID = "id-one"
		otherClusterID    = "id-two"
	)

	providerExport := makeExport(cfg, "foo-one", providerClusterID, testProviderName)
	// any workspace can define its own content-for label values:
	otherExport := makeExport(cfg, "foo-two", otherClusterID, testProviderName)

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(makeProviderMeta(providerClusterID), providerExport, otherExport).
		Build()

	list := listMarketplace(t, lister, fake.NewClientBuilder().WithScheme(s).Build(), cfg)

	require.Len(t, list.Items, 1)

	exportCluster, found, err := unstructured.NestedString(
		list.Items[0].Object, "spec", "apiExport", "metadata", "annotations", "kcp.io/cluster")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, providerClusterID, exportCluster)
}

func TestMarketplace_ExportsOfKnownProviders(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(makeProviderMeta(testProviderClusterID), makeDefaultExport(cfg)).
		Build()

	bindings := fake.NewClientBuilder().WithScheme(s).Build()

	list := listMarketplace(t, lister, bindings, cfg)

	require.Len(t, list.Items, 1)
	// convention might change, max len etc:
	assert.Equal(t, testExportName+"-"+testProviderName, list.Items[0].GetName())
}

func TestMarketplace_SkipsExportsWithoutLegacyCRDResources(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)
	export := makeDefaultExport(cfg)
	export.Spec.Resources[0].Storage = kcpapisv1alpha2.ResourceSchemaStorage{
		Virtual: &kcpapisv1alpha2.ResourceSchemaStorageVirtual{},
	}

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(makeProviderMeta(testProviderClusterID), export).
		Build()

	list := listMarketplace(t, lister, fake.NewClientBuilder().WithScheme(s).Build(), cfg)

	assert.Empty(t, list.Items, "virtual-storage-only exports are not compatible with legacy marketplace clients")
}

func TestMarketplace_PreservesAPIExportDefaultSelector(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)
	export := makeDefaultExport(cfg)
	export.Spec.PermissionClaims = []kcpapisv1alpha2.PermissionClaim{
		{
			GroupResource: kcpapisv1alpha2.GroupResource{Resource: "secrets"},
			Verbs:         []string{"get", "create", "update", "patch"},
			DefaultSelector: &kcpapisv1alpha2.PermissionClaimSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"example.io/credential": "true",
					},
					MatchExpressions: []metav1.LabelSelectorRequirement{{
						Key:      "example.io/environment",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"development", "testing"},
					}},
				},
			},
		},
		{
			GroupResource: kcpapisv1alpha2.GroupResource{Resource: "events"},
			Verbs:         []string{"*"},
		},
		{
			GroupResource: kcpapisv1alpha2.GroupResource{Resource: "configmaps"},
			Verbs:         []string{"*"},
			DefaultSelector: &kcpapisv1alpha2.PermissionClaimSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"example.io/config": "true"},
				},
			},
		},
	}

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(makeProviderMeta(testProviderClusterID), export).
		Build()

	list := listMarketplace(t, lister, fake.NewClientBuilder().WithScheme(s).Build(), cfg)

	require.Len(t, list.Items, 1)
	legacyAPIVersion, found, err := unstructured.NestedString(
		list.Items[0].Object, "spec", "apiExport", "apiVersion")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "apis.kcp.io/v1alpha1", legacyAPIVersion)
	legacyKind, found, err := unstructured.NestedString(
		list.Items[0].Object, "spec", "apiExport", "kind")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "APIExport", legacyKind)

	claims, found, err := unstructured.NestedSlice(
		list.Items[0].Object, "spec", "apiExportPermissionClaims")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, claims, 3)
	claim, ok := claims[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"get", "create", "update", "patch"}, claim["verbs"])
	assert.Equal(t, map[string]any{
		"matchLabels": map[string]any{"example.io/credential": "true"},
		"matchExpressions": []any{map[string]any{
			"key":      "example.io/environment",
			"operator": "In",
			"values":   []any{"development", "testing"},
		}},
	}, claim["defaultSelector"])
	_, hasDefaultSelector := claims[1].(map[string]any)["defaultSelector"]
	assert.False(t, hasDefaultSelector)

	legacyClaims, found, err := unstructured.NestedSlice(
		list.Items[0].Object, "spec", "apiExport", "spec", "permissionClaims")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, legacyClaims, 1, "legacy clients must not receive claims whose scope or verbs v1alpha1 cannot represent")
	assert.Equal(t, map[string]any{
		"resource": "events",
		"all":      true,
	}, legacyClaims[0])

	latestSchemas, found, err := unstructured.NestedStringSlice(
		list.Items[0].Object, "spec", "apiExport", "spec", "latestResourceSchemas")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []string{"v260810" + testExportName}, latestSchemas)
}

func TestMarketplace_OmitsV1Alpha1ResourceSelectorClaims(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)
	legacyExport := &kcpapisv1alpha1.APIExport{
		ObjectMeta: metav1.ObjectMeta{
			Name: testExportName,
			Annotations: map[string]string{
				"kcp.io/cluster": testProviderClusterID,
			},
			Labels: map[string]string{cfg.ContentForLabel: testProviderName},
		},
		Spec: kcpapisv1alpha1.APIExportSpec{
			LatestResourceSchemas: []string{"v260810" + testExportName},
			PermissionClaims: []kcpapisv1alpha1.PermissionClaim{
				{
					GroupResource: kcpapisv1alpha1.GroupResource{Resource: "secrets"},
					ResourceSelector: []kcpapisv1alpha1.ResourceSelector{{
						Name:      "provider-credential",
						Namespace: "default",
					}},
				},
				{
					GroupResource: kcpapisv1alpha1.GroupResource{Resource: "events"},
					All:           true,
				},
			},
		},
	}
	export := &kcpapisv1alpha2.APIExport{}
	require.NoError(t, kcpapisv1alpha2.Convert_v1alpha1_APIExport_To_v1alpha2_APIExport(legacyExport, export, nil))
	require.Contains(t, export.Annotations, kcpapisv1alpha2.PermissionClaimsV1Alpha1Annotation)

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(makeProviderMeta(testProviderClusterID), export).
		Build()

	list := listMarketplace(t, lister, fake.NewClientBuilder().WithScheme(s).Build(), cfg)

	require.Len(t, list.Items, 1)
	exactClaims, found, err := unstructured.NestedSlice(
		list.Items[0].Object, "spec", "apiExportPermissionClaims")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []any{map[string]any{
		"resource": "events",
		"verbs":    []any{"*"},
	}}, exactClaims, "v1alpha1 resourceSelector must not be broadened to a v1alpha2 match-all claim")

	legacyClaims, found, err := unstructured.NestedSlice(
		list.Items[0].Object, "spec", "apiExport", "spec", "permissionClaims")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []any{map[string]any{
		"resource": "events",
		"all":      true,
	}}, legacyClaims, "old Marketplace clients turn every exposed legacy claim into matchAll")
}

func TestProjectPermissionClaimsFailsClosedOnDuplicateKeys(t *testing.T) {
	claims := []kcpapisv1alpha2.PermissionClaim{
		{
			GroupResource: kcpapisv1alpha2.GroupResource{Resource: "secrets"},
			Verbs:         []string{"*"},
		},
		{
			GroupResource: kcpapisv1alpha2.GroupResource{Resource: "secrets"},
			Verbs:         []string{"get"},
		},
	}

	legacyClaims, exactClaims := projectPermissionClaims(claims, nil)

	assert.Empty(t, legacyClaims)
	assert.Empty(t, exactClaims)
}

func TestMarketplace_PreservesProviderDetailViewExtensions(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)
	provider := makeProviderMeta(testProviderClusterID)
	provider.Spec.DetailViewExtensions = []pmuiv1alpha1.DetailViewExtension{
		{URL: "https://provider.example/details"},
		{URL: "https://provider.example/compatibility"},
	}

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(provider, makeDefaultExport(cfg)).
		Build()

	list := listMarketplace(t, lister, fake.NewClientBuilder().WithScheme(s).Build(), cfg)

	require.Len(t, list.Items, 1)
	extensions, found, err := unstructured.NestedSlice(
		list.Items[0].Object, "spec", "providerMetadata", "spec", "detailViewExtensions")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, extensions, 2)
	assert.Equal(
		t, "https://provider.example/details",
		extensions[0].(map[string]any)["url"],
	)
	assert.Equal(
		t, "https://provider.example/compatibility",
		extensions[1].(map[string]any)["url"],
	)
}

func TestMarketplace_InstalledBinding(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(makeProviderMeta(testProviderClusterID), makeDefaultExport(cfg)).
		Build()

	bindings := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(&kcpapisv1alpha1.APIBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "widgets-binding"},
			Spec: kcpapisv1alpha1.APIBindingSpec{
				Reference: kcpapisv1alpha1.BindingReference{
					Export: &kcpapisv1alpha1.ExportBindingReference{Name: testExportName},
				},
			},
			Status: kcpapisv1alpha1.APIBindingStatus{
				APIExportClusterName: testProviderClusterID,
			},
		}).
		Build()

	list := listMarketplace(t, lister, bindings, cfg)

	require.Len(t, list.Items, 1)
	name, found, err := unstructured.NestedString(list.Items[0].Object, "spec", "apiBindingName")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "widgets-binding", name)
}

func TestMarketplace_ExportsWithoutMatchingProvider(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)

	orphanedExport := makeExport(cfg, testExportName, "foocluster-123", "unknown-provider")

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(makeProviderMeta("whatever"), orphanedExport).
		Build()

	list := listMarketplace(t, lister, fake.NewClientBuilder().WithScheme(s).Build(), cfg)

	assert.Empty(t, list.Items, "should skip unknown providers")
}
