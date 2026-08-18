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
	testResourceName      = "widgets"
	testResourceGroup     = "abcd.io"
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
				Name:   testResourceName,
				Group:  testResourceGroup,
				Schema: "v260810." + testResourceName + "." + testResourceGroup,
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
