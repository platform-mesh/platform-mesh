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

package resolver

import (
	"errors"
	"fmt"

	"github.com/graphql-go/graphql"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type TypeByCategory struct {
	Group   string
	Version string
	Kind    string
	Scope   string
}

func (r *Service) TypeByCategory(m map[string][]TypeByCategory) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		name, err := GetArg[string](p.Args, NameArg, true)
		if err != nil {
			return nil, err
		}

		return m[name], nil
	}
}

func (r *Service) ResourcesByCategory(m map[string][]TypeByCategory) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		name, err := GetArg[string](p.Args, NameArg, true)
		if err != nil {
			return nil, err
		}

		members := m[name]
		result := make([]map[string]any, 0)

		for _, v := range members {
			gvk := schema.GroupVersionKind{Group: v.Group, Version: v.Version, Kind: v.Kind}
			scope := apiextensionsv1.ResourceScope(v.Scope)
			items, err := r.ListItems(gvk, scope)(p)
			if err != nil {
				return nil, fmt.Errorf("getting items: %w", err)
			}

			listresult, ok := items.(*ListResult)
			if !ok {
				return nil, errors.New("ListItems returned wrong type: was not *ListResult")
			}

			result = append(result, listresult.Items...)

		}

		return result, nil
	}
}
