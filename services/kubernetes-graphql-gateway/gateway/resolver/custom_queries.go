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
	"fmt"
	"sync"

	"github.com/graphql-go/graphql"
	"golang.org/x/sync/errgroup"

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

		// parallel := len(members)

		grp, ctx := errgroup.WithContext(p.Context)
		p.Context = ctx
		// revisit after poc: floor(members, maxParallel)
		grp.SetLimit(len(members))

		gather := make([][]map[string]any, len(members))

		for i, v := range members {
			grp.Go(func() error {
				gvk := schema.GroupVersionKind{Group: v.Group, Version: v.Version, Kind: v.Kind}
				scope := apiextensionsv1.ResourceScope(v.Scope)
				items, err := r.ListItems(gvk, scope)(p)
				if err != nil {
					return fmt.Errorf("getting items for gvk %s: %w", gvk, err)
				}

				listresult, ok := items.(*ListResult)
				if !ok {
					return fmt.Errorf("ListItems returned wrong type: expected *ListResult got %T", items)
				}

				gather[i] = listresult.Items
				return nil
			})
		}

		err = grp.Wait()
		if err != nil {
			return nil, err
		}

		var result []map[string]any
		for _, v := range gather {
			result = append(result, v...)
		}

		return result, nil
	}
}

func (r *Service) SubscribeResourcesByCategory(m map[string][]TypeByCategory) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		category, err := GetArg[string](p.Args, NameArg, true)
		if err != nil {
			return nil, fmt.Errorf("no name arg in %v; %w", p.Args, err)
		}

		categoryTypes := m[category]
		categoryTypesCount := len(categoryTypes)
		outCh := make(chan any, categoryTypesCount)

		if r.metrics != nil {
			r.metrics.UpdateCategoryWatches(category, categoryTypesCount)
		}

		var wg sync.WaitGroup
		for _, cType := range categoryTypes {
			gvk := schema.GroupVersionKind{Group: cType.Group, Version: cType.Version, Kind: cType.Kind}
			scope := apiextensionsv1.ResourceScope(cType.Scope)

			sub, err := r.SubscribeItems(gvk, scope)(p)
			if err != nil {
				// TODO: only ignore RBAC errors
				continue
			}
			srcChan, ok := sub.(chan any)
			if !ok {
				// TODO: error log
				continue
			}

			wg.Go(func() {
				for {
					select {
					case event, ok := <-srcChan:
						if !ok {
							return
						}
						select {
						case outCh <- event:
						case <-p.Context.Done():
							return
						}
					case <-p.Context.Done():
						return
					}
				}
			})
		}

		go func() {
			wg.Wait()
			if r.metrics != nil {
				r.metrics.UpdateCategoryWatches(category, -categoryTypesCount)
			}
			close(outCh)
		}()

		return outCh, nil
	}
}
