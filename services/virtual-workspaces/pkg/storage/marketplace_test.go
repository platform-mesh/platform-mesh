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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
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
	"github.com/kcp-dev/virtual-workspace-framework/pkg/forwardingregistry"
)

// Default user and org cluster IDs for PM users.
const (
	testOrgClusterID     = "acme-org-cluster-id"
	testAccountClusterID = "user-cluster-id"
)

// Default provider values.
const (
	testProviderClusterID = "abcd-provider-cluster-id"
	testProviderName      = "abcd"
	testExportName        = "widgets.abcd.io"
)

func marketplaceTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	utilruntime.Must(pmuiv1alpha1.AddToScheme(s))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(s))
	utilruntime.Must(pmcorev1alpha1.AddToScheme(s))
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
func makeDefaultExport(cfg config.ServiceConfig) *kcpapisv1alpha1.APIExport {
	return makeExport(cfg, testExportName, testProviderClusterID, testProviderName)
}

// makeExport returns an APIExport with the given name and provider label.
func makeExport(cfg config.ServiceConfig, name, clusterID, providerLabel string) *kcpapisv1alpha1.APIExport {
	return &kcpapisv1alpha1.APIExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{"kcp.io/cluster": clusterID},
			Labels:      map[string]string{cfg.ContentForLabel: providerLabel},
		},
		Spec: kcpapisv1alpha1.APIExportSpec{
			LatestResourceSchemas: []string{"v260810" + name},
		},
	}
}

// makeAccountInfo returns the AccountInfo created in an account's own workspace.
func makeAccountInfo(accountClusterID, orgClusterID string) pmcorev1alpha1.AccountInfo {
	org := pmcorev1alpha1.AccountLocation{
		Name:               "acme",
		Path:               "root:orgs:acme",
		GeneratedClusterId: orgClusterID,
		OriginClusterId:    "root-orgs-ID",
	}
	return pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: accountInfoName},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			Account: pmcorev1alpha1.AccountLocation{
				Name:               "user",
				Path:               org.Path + ":user",
				GeneratedClusterId: accountClusterID,
			},
			ParentAccount: &org,
			Organization:  org,
		},
	}
}

// makePolicy grants the caller organization visibility of the given exports.
func makePolicy(caller pmcorev1alpha1.AccountInfo, exports ...*kcpapisv1alpha1.APIExport) pmcorev1alpha1.ProviderVisibilityPolicy {
	var clusterOrder []string
	namesByCluster := map[string][]string{}
	for _, export := range exports {
		clusterID := export.Annotations["kcp.io/cluster"]
		if _, seen := namesByCluster[clusterID]; !seen {
			clusterOrder = append(clusterOrder, clusterID)
		}
		namesByCluster[clusterID] = append(namesByCluster[clusterID], export.Name)
	}

	resolved := make([]pmcorev1alpha1.ResolvedProviderExport, 0, len(clusterOrder))
	for _, clusterID := range clusterOrder {
		resolved = append(resolved, pmcorev1alpha1.ResolvedProviderExport{
			ClusterID:      clusterID,
			APIExportNames: namesByCluster[clusterID],
		})
	}

	return pmcorev1alpha1.ProviderVisibilityPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "policy-" + exports[0].Name},
		Status: pmcorev1alpha1.ProviderVisibilityPolicyStatus{
			AccountClusterID:        caller.Spec.Organization.GeneratedClusterId,
			ResolvedProviderExports: resolved,
		},
	}
}

// listMarketplace decorates and runs the Marketplace func.
func listMarketplace(
	t *testing.T,
	cfg config.ServiceConfig,
	lister ctrlruntimeclient.Client,
	caller pmcorev1alpha1.AccountInfo,
	extraObjects ...ctrlruntimeclient.Object,
) *unstructured.UnstructuredList {
	t.Helper()
	callerCluster := caller.Spec.Account.GeneratedClusterId

	getClusterClient := func(ctx context.Context, name string) (ctrlruntimeclient.Client, error) {
		if name != callerCluster {
			return nil, fmt.Errorf("getClusterClient for %q, expected to equal caller cluster %q", name, callerCluster)
		}
		clientSchema := marketplaceTestScheme(t)
		return fake.NewClientBuilder().
				WithScheme(clientSchema).
				WithObjects(append([]ctrlruntimeclient.Object{&caller}, extraObjects...)...).
				Build(),
			nil
	}

	var funcs forwardingregistry.StoreFuncs
	Marketplace(lister, getClusterClient, cfg).
		Decorate(schema.GroupResource{}, &funcs)

	ctx := genericapirequest.WithCluster(t.Context(), genericapirequest.Cluster{
		Name: logicalcluster.Name(callerCluster),
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
		// The correct provider cluster.
		expectedProviderClusterID = "id-one"
		// A cluster with an export having a duplicate content-for label.
		otherProviderClusterID = "id-two"
	)

	providerExport := makeExport(cfg, testExportName, expectedProviderClusterID, testProviderName)
	// any workspace can define its own content-for label values:
	foreignExport := makeExport(cfg, "backups.other.io", otherProviderClusterID, testProviderName)

	caller := makeAccountInfo(testAccountClusterID, testOrgClusterID)
	// allow both exports
	policy := makePolicy(caller, providerExport, foreignExport)

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(
			makeProviderMeta(expectedProviderClusterID),
			providerExport,
			foreignExport,
			&policy,
		).
		Build()

	list := listMarketplace(t, cfg, lister, caller)

	require.Len(t, list.Items, 1, "an entitled export from another cluster must not join the provider's card")
	assert.Equal(t, testExportName+"-"+testProviderName, list.Items[0].GetName())
}

func TestMarketplace_ExportsOfKnownProviders(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)

	caller := makeAccountInfo(testAccountClusterID, testOrgClusterID)
	export := makeDefaultExport(cfg)
	policy := makePolicy(caller, export)

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(makeProviderMeta(testProviderClusterID), export, &policy).
		Build()

	list := listMarketplace(t, cfg, lister, caller)

	require.Len(t, list.Items, 1, "an entitled export must appear in the catalogue")
	// convention might change, max len etc:
	assert.Equal(t, testExportName+"-"+testProviderName, list.Items[0].GetName())
}

func TestMarketplace_InstalledBinding(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)

	caller := makeAccountInfo(testAccountClusterID, testOrgClusterID)
	export := makeDefaultExport(cfg)
	policy := makePolicy(caller, export)

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(makeProviderMeta(testProviderClusterID), export, &policy).
		Build()

	binding := kcpapisv1alpha1.APIBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "widgets-binding"},
		Spec: kcpapisv1alpha1.APIBindingSpec{
			Reference: kcpapisv1alpha1.BindingReference{
				Export: &kcpapisv1alpha1.ExportBindingReference{Name: export.Name},
			},
		},
		Status: kcpapisv1alpha1.APIBindingStatus{
			APIExportClusterName: export.Annotations["kcp.io/cluster"],
		},
	}

	list := listMarketplace(t, cfg, lister, caller, &binding)

	require.Len(t, list.Items, 1, "an entitled export must appear in the catalogue")
	name, found, err := unstructured.NestedString(list.Items[0].Object, "spec", "apiBindingName")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "widgets-binding", name, "an export the caller already binds must report its binding")
}

func TestMarketplace_ExportsWithoutMatchingProvider(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)

	orphanedExport := makeExport(cfg, testExportName, "foocluster-123", "unknown-provider")

	caller := makeAccountInfo(testAccountClusterID, testOrgClusterID)
	policy := makePolicy(caller, orphanedExport)

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(makeProviderMeta("whatever"), orphanedExport, &policy).
		Build()

	list := listMarketplace(t, cfg, lister, caller)

	assert.Empty(t, list.Items, "an entitled export whose provider is unknown must not be listed")
}

func TestMarketplace_ExportsWithoutPolicy(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(makeProviderMeta(testProviderClusterID), makeDefaultExport(cfg)).
		Build()

	caller := makeAccountInfo(testAccountClusterID, testOrgClusterID)
	list := listMarketplace(t, cfg, lister, caller)

	assert.Empty(t, list.Items, "an account with no policy in org must see nothing")
}

func TestMarketplace_ExportsFromMultiplePolicies(t *testing.T) {
	cfg := config.NewServiceConfig()
	s := marketplaceTestScheme(t)

	const secondExportName = "backups.abcd.io"

	caller := makeAccountInfo(testAccountClusterID, testOrgClusterID)
	widgetsExport := makeDefaultExport(cfg)
	backupsExport := makeExport(cfg, secondExportName, testProviderClusterID, testProviderName)

	widgetsPolicy := makePolicy(caller, widgetsExport)
	backupsPolicy := makePolicy(caller, backupsExport)

	lister := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(
			makeProviderMeta(testProviderClusterID),
			widgetsExport,
			backupsExport,
			&widgetsPolicy,
			&backupsPolicy,
		).
		Build()

	list := listMarketplace(t, cfg, lister, caller)

	require.Len(t, list.Items, 2, "policies naming the same provider cluster must result in the union")

	var names []string
	for _, entry := range list.Items {
		names = append(names, entry.GetName())
	}
	assert.ElementsMatch(t, []string{
		testExportName + "-" + testProviderName,
		secondExportName + "-" + testProviderName,
	}, names)
}
