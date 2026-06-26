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

package extensions

import (
	"errors"
	"fmt"

	pmgateway "go.platform-mesh.io/apis/gateway"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/resolver"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

type CategoryManager struct {
	definitions    map[string]*spec.Schema
	typeByCategory map[string][]resolver.TypeByCategory
}

func NewCategoryManager(definitions map[string]*spec.Schema) *CategoryManager {
	return &CategoryManager{
		definitions:    definitions,
		typeByCategory: make(map[string][]resolver.TypeByCategory),
	}
}

func (m *CategoryManager) Store(
	resourceKey string,
	gvk *schema.GroupVersionKind,
	resourceScope apiextensionsv1.ResourceScope,
) error {
	resourceSpec, ok := m.definitions[resourceKey]
	if !ok || resourceSpec.Extensions == nil {
		return errors.New("no resource extensions")
	}

	categoriesRaw, ok := resourceSpec.Extensions[pmgateway.CategoriesExtensionKey]
	if !ok {
		return fmt.Errorf("%s extension not found", pmgateway.CategoriesExtensionKey)
	}

	categoriesRawArray, ok := categoriesRaw.([]any)
	if !ok {
		return fmt.Errorf("%s extension is not an array", pmgateway.CategoriesExtensionKey)
	}

	categories := make([]string, len(categoriesRawArray))
	for i, v := range categoriesRawArray {
		if str, ok := v.(string); ok {
			categories[i] = str
		} else {
			return fmt.Errorf("failed to convert %v to string", v)
		}
	}

	for _, category := range categories {
		m.typeByCategory[category] = append(m.typeByCategory[category], resolver.TypeByCategory{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind,
			Scope:   string(resourceScope),
		})
	}

	return nil
}

func (m *CategoryManager) AllCategories() map[string][]resolver.TypeByCategory {
	return m.typeByCategory
}
