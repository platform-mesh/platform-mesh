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

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apiextensions-apiserver/pkg/registry/customresource"
	"k8s.io/apimachinery/pkg/api/validate/content"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/rest"

	"github.com/kcp-dev/client-go/dynamic"
	"github.com/kcp-dev/virtual-workspace-framework/pkg/dynamic/apiserver"
	registry "github.com/kcp-dev/virtual-workspace-framework/pkg/forwardingregistry"
)

func CreateStorageProviderFunc(clusterClient dynamic.ClusterInterface, filters ...registry.StorageWrapper) func(ctx context.Context) (apiserver.RestProviderFunc, error) {
	return func(ctx context.Context) (apiserver.RestProviderFunc, error) {
		return func(resource schema.GroupVersionResource, kind, listKind schema.GroupVersionKind, typer runtime.ObjectTyper, tableConvertor rest.TableConvertor, namespaceScoped bool, schemaValidator validation.SchemaValidator, subresourcesSchemaValidator map[string]validation.SchemaValidator, structuralSchema *structuralschema.Structural) (mainStorage rest.Storage, subresourceStorages map[string]rest.Storage) {
			statusSchemaValidate, statusEnabled := subresourcesSchemaValidator["status"]
			var statusSpec *apiextensions.CustomResourceSubresourceStatus
			if statusEnabled {
				statusSpec = &apiextensions.CustomResourceSubresourceStatus{}
			}

			strategy := customresource.NewStrategy(
				typer,
				namespaceScoped,
				kind,
				func(name string, prefix bool) []string {
					if prefix {
						return content.IsPathSegmentPrefix(name)
					}
					return content.IsPathSegmentName(name)
				},
				schemaValidator,
				statusSchemaValidate,
				structuralSchema,
				statusSpec,
				nil,
				[]apiextensionsv1.SelectableField{},
			)

			wrappers := make(registry.StorageWrappers, 0, len(filters))
			for _, filter := range filters {
				wrappers = append(wrappers, filter)
			}

			storage, statusStorage := registry.NewStorage(
				ctx,
				resource,
				"",
				kind,
				listKind,
				strategy,
				nil,
				tableConvertor,
				nil,
				func(ctx context.Context) (dynamic.ClusterInterface, error) {
					return clusterClient, nil
				},
				nil,
				&wrappers,
			)

			// we want to expose some but not all the allowed endpoints,
			// so filter by exposing just the funcs we need
			subresourceStorages = make(map[string]rest.Storage)
			if statusEnabled {
				subresourceStorages["status"] = &struct {
					registry.FactoryFunc
					registry.DestroyerFunc

					registry.GetterFunc

					registry.TableConvertorFunc
					registry.CategoriesProviderFunc
					registry.ResetFieldsStrategyFunc
				}{
					FactoryFunc:   statusStorage.FactoryFunc,
					DestroyerFunc: statusStorage.DestroyerFunc,

					GetterFunc: statusStorage.GetterFunc,

					TableConvertorFunc:      statusStorage.TableConvertorFunc,
					CategoriesProviderFunc:  statusStorage.CategoriesProviderFunc,
					ResetFieldsStrategyFunc: statusStorage.ResetFieldsStrategyFunc,
				}
			}

			// only expose GET/LIST (list needs watch)
			return &struct {
				registry.FactoryFunc
				registry.ListFactoryFunc
				registry.DestroyerFunc

				registry.GetterFunc
				registry.ListerFunc
				registry.WatcherFunc

				registry.TableConvertorFunc
				registry.CategoriesProviderFunc
				registry.ResetFieldsStrategyFunc
			}{
				FactoryFunc:     storage.FactoryFunc,
				ListFactoryFunc: storage.ListFactoryFunc,
				DestroyerFunc:   storage.DestroyerFunc,

				GetterFunc:  storage.GetterFunc,
				ListerFunc:  storage.ListerFunc,
				WatcherFunc: storage.WatcherFunc,

				TableConvertorFunc:      storage.TableConvertorFunc,
				CategoriesProviderFunc:  storage.CategoriesProviderFunc,
				ResetFieldsStrategyFunc: storage.ResetFieldsStrategyFunc,
			}, subresourceStorages
		}, nil
	}
}
