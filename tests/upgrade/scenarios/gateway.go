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

package scenarios

import (
	"context"
	"fmt"
	"slices"

	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"
)

// Steps that read through the GraphQL gateway, which is how the portal
// reads: it serves every workspace as a GraphQL schema generated from the
// APIs of the workspace, and sends the queries on to kcp as the user.

// gatewayPath is where the gateway serves the workspace at path.
func gatewayPath(path string) string { return "/gateway/api/clusters/" + path + "/graphql" }

// validateGateway: the user, logged in, lists the accounts of the
// organization through the gateway, and they are the ones named.
func validateGateway(org, email string, accounts ...string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		token, err := login(ctx, env, org, email)
		if err != nil {
			return err
		}
		var answer struct {
			Core struct {
				V1alpha1 struct {
					Accounts struct {
						Items []struct{ Metadata struct{ Name string } }
					} `json:"Accounts"`
				} `json:"v1alpha1"`
			} `json:"core_platform_mesh_io"`
		}
		query := `{ core_platform_mesh_io { v1alpha1 { Accounts { items { metadata { name } } } } } }`
		path := tenant.OrganizationPath(org)
		if err := graphQL(ctx, env, org, gatewayPath(path), token, query, &answer); err != nil {
			return fmt.Errorf("the accounts of %s through the gateway: %w", path, err)
		}
		got := make([]string, 0, len(answer.Core.V1alpha1.Accounts.Items))
		for _, item := range answer.Core.V1alpha1.Accounts.Items {
			got = append(got, item.Metadata.Name)
		}
		slices.Sort(got)
		want := slices.Sorted(slices.Values(accounts))
		if !slices.Equal(got, want) {
			return steps.Terminal("the gateway lists the accounts %v of %s, not %v", got, path, want)
		}
		return nil
	}
}
