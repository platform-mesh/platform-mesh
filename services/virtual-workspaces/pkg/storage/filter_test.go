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

	kcpdynamic "github.com/kcp-dev/client-go/dynamic"
	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.platform-mesh.io/virtual-workspaces/pkg/config"

	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"

	"github.com/kcp-dev/virtual-workspace-framework/pkg/forwardingregistry"

	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func newCC(name string, ccLabels map[string]string, hasResult bool) unstructured.Unstructured {
	obj := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "ui.platform-mesh.io/v1alpha1",
			"kind":       "ContentConfiguration",
			"metadata": map[string]interface{}{
				"name":   name,
				"labels": toInterfaceMap(ccLabels),
			},
		},
	}
	if hasResult {
		obj.Object["status"] = map[string]interface{}{
			"configurationResult": map[string]interface{}{
				"nodes": []interface{}{},
			},
		}
	}
	return obj
}

func toInterfaceMap(m map[string]string) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// fakeDynamicClusterClient implements kcpdynamic.ClusterInterface for testing.
// It returns an empty APIBinding list so the export-workspace loop is a no-op.
type fakeDynamicClusterClient struct {
	kcpdynamic.ClusterInterface
}

func (f *fakeDynamicClusterClient) Cluster(_ logicalcluster.Path) dynamic.Interface {
	return &fakeDynamicInterface{}
}

type fakeDynamicInterface struct {
	dynamic.Interface
}

func (f *fakeDynamicInterface) Resource(_ schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &fakeNamespaceableResource{}
}

type fakeNamespaceableResource struct {
	dynamic.NamespaceableResourceInterface
}

func (f *fakeNamespaceableResource) List(_ context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{}, nil
}

func (f *fakeNamespaceableResource) Watch(_ context.Context, _ metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

// clusterAwareLister creates a mock ListerFunc that returns allCCs only when called
// from the original workspace context (cluster name matches accountCluster), and
// returns empty lists for other cluster contexts (export workspaces, provider workspace).
// It applies label selector filtering to simulate kcp apiserver behavior.
func clusterAwareLister(allCCs []unstructured.Unstructured, accountCluster logicalcluster.Name) forwardingregistry.ListerFunc {
	return func(ctx context.Context, opts *internalversion.ListOptions) (runtime.Object, error) {
		cluster := genericapirequest.ClusterFrom(ctx)

		// Only return CCs for the account workspace; other clusters return empty.
		var candidates []unstructured.Unstructured
		if cluster != nil && cluster.Name == accountCluster {
			candidates = allCCs
		}

		result := &unstructured.UnstructuredList{}
		for _, cc := range candidates {
			ccLabels := cc.GetLabels()
			if opts != nil && opts.LabelSelector != nil {
				if !opts.LabelSelector.Matches(labels.Set(ccLabels)) {
					continue
				}
			}
			result.Items = append(result.Items, cc)
		}
		return result, nil
	}
}

func TestContentConfigurationLookup_ExcludesContentForFromLocalWorkspace(t *testing.T) {
	t.Parallel()

	cfg := config.NewServiceConfig()
	accountCluster := logicalcluster.Name("my-account")

	localOnlyCC := newCC("local-dashboard", nil, true)
	providerProjectedCC := newCC("crossplane-objects", map[string]string{
		cfg.ContentForLabel: "openmcp.cloud",
		cfg.EntityLabel:     cfg.AccountEntityName,
	}, true)

	tests := []struct {
		name          string
		allCCs        []unstructured.Unstructured
		expectedNames []string
	}{
		{
			name:          "only local CCs returned when content-for CCs are present",
			allCCs:        []unstructured.Unstructured{localOnlyCC, providerProjectedCC},
			expectedNames: []string{"local-dashboard"},
		},
		{
			name:          "all local CCs returned when no content-for CCs exist",
			allCCs:        []unstructured.Unstructured{localOnlyCC},
			expectedNames: []string{"local-dashboard"},
		},
		{
			name:          "empty result when all CCs have content-for label",
			allCCs:        []unstructured.Unstructured{providerProjectedCC},
			expectedNames: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			storage := &forwardingregistry.StoreFuncs{}
			storage.ListerFunc = clusterAwareLister(tt.allCCs, accountCluster)

			wrapper := ContentConfigurationLookup(&fakeDynamicClusterClient{}, cfg, "provider-ws")
			wrapper.Decorate(schema.GroupResource{Group: "ui.platform-mesh.io", Resource: "contentconfigurations"}, storage)

			ctx := WithClusterPath(context.Background(), logicalcluster.NewPath("root:orgs:my-org:my-account"))
			ctx = genericapirequest.WithCluster(ctx, genericapirequest.Cluster{
				Name: accountCluster,
			})

			result, err := storage.List(ctx, &internalversion.ListOptions{})
			require.NoError(t, err)

			ul := result.(*unstructured.UnstructuredList)
			var gotNames []string
			for _, item := range ul.Items {
				gotNames = append(gotNames, item.GetName())
			}

			assert.Equal(t, tt.expectedNames, gotNames)
		})
	}
}

func TestContentConfigurationWithResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		items         []unstructured.Unstructured
		expectedNames []string
	}{
		{
			name: "includes CCs with configurationResult",
			items: []unstructured.Unstructured{
				newCC("with-result", nil, true),
			},
			expectedNames: []string{"with-result"},
		},
		{
			name: "excludes CCs without configurationResult",
			items: []unstructured.Unstructured{
				newCC("no-result", nil, false),
			},
			expectedNames: nil,
		},
		{
			name: "filters mixed CCs",
			items: []unstructured.Unstructured{
				newCC("with-result", nil, true),
				newCC("no-result", nil, false),
				newCC("another-with-result", nil, true),
			},
			expectedNames: []string{"with-result", "another-with-result"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ul := &unstructured.UnstructuredList{Items: tt.items}
			result := contentConfigurationWithResult(ul)

			var gotNames []string
			for _, item := range result {
				gotNames = append(gotNames, item.GetName())
			}

			assert.Equal(t, tt.expectedNames, gotNames)
		})
	}
}
