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
	"slices"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/gateway"
	"go.platform-mesh.io/platform-mesh/tests/framework/steps"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Steps that read through the GraphQL gateway (tests/framework/gateway),
// which is how the portal reads.

// accounts is the Account API as the schema of the gateway spells it, with
// the kind in the plural.
var accounts = pmcorev1alpha1.SchemeGroupVersion.WithKind("Accounts")

// validateGateway: the user, logged in to the organization, lists the
// objects of the kind in the workspace at path through the gateway, and
// they are the ones named. kinds is the kind as the gateway spells it (see
// gateway.List). The gateway answers only for an API it has in the schema
// it generated for the workspace.
func validateGateway(org, email, path string, kinds schema.GroupVersionKind, names ...string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		token, err := login(ctx, env, org, email)
		if err != nil {
			return err
		}
		got, err := gateway.List(ctx, env, org, path, token, kinds)
		if err != nil {
			return err
		}
		if want := slices.Sorted(slices.Values(names)); !slices.Equal(got, want) {
			return steps.Terminal("the gateway lists the %s %v of %s, not %v", kinds.Kind, got, path, want)
		}
		return nil
	}
}
