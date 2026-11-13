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

	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The provider scenario: the marketplace of the release. A provider has a
// workspace in root:providers and exports an API from it, the Widget; a
// policy lets the accounts of one organization bind it; an account of that
// organization has bound it, as the portal does for "Enable" in the
// marketplace, and its owner has created a Widget.
//
// After the upgrade the binding is bound, the Widget is there, and the
// owner, logged in, still works with Widgets, through kcp and through the
// gateway, which has to serve the API of the provider in the schema of the
// account. A guest of the organization does not get at them.
//
// No controller of the provider runs: this is about what Platform Mesh does
// for a provider API, the binding, the authorization model generated for
// it, and the schema of the gateway, not about a provider.
//
// The objects are in manifests/provider-*.yaml and manifests/consumer-*.yaml.
const (
	providerWorkspace = "upgrade-seed"
	providerExport    = "seed.upgrade.platform-mesh.io"

	consumerOrg     = "upgrade-consumer"
	consumerOwner   = "owner@upgrade-consumer.example.com"
	consumerGuest   = "guest@upgrade-consumer.example.com"
	consumerAccount = "app"

	// providersPath is the workspace the workspaces of providers are in.
	providersPath = "root:providers"
)

// widgets is the API of the provider.
var widgets = schema.GroupVersionKind{Group: providerExport, Version: "v1alpha1", Kind: "Widget"}

// Provider is the provider scenario.
func Provider() Scenario {
	var (
		provider = providersPath + ":" + providerWorkspace
		org      = tenant.OrganizationPath(consumerOrg)
		account  = tenant.AccountPath(consumerOrg, consumerAccount)
	)
	return Scenario{
		Name: "provider",

		// ORDER MATTERS. Every step builds on the ones before it.
		Seed: []Step{
			// 1. The provider: its workspace, then the API it exports
			//    from it.
			{Name: "workspace of the provider", Run: seedWorkspace(providersPath, "provider-workspace.yaml", providerWorkspace)},
			{Name: "export of the provider", Run: seedManifest(provider, "provider-export.yaml")},
			// 2. The consumer: an organization with an owner who can
			//    log in, a guest, and an account.
			{Name: "organization", Run: seedOrganization(consumerOrg, consumerOwner)},
			{Name: "password of the owner", Run: seedPassword(consumerOrg, consumerOwner)},
			{Name: "invite of the guest", Run: seedInvite(consumerOrg, "guest", consumerGuest)},
			{Name: "password of the guest", Run: seedPassword(consumerOrg, consumerGuest)},
			{Name: "account", Run: seedAccount(org, consumerAccount, consumerOwner)},
			// 3. After the export and the organization, which it names:
			//    the policy that lets the accounts of the organization
			//    bind the export. It lives in root:orgs.
			{Name: "policy of the export", Run: seedPolicy()},
			// 4. After the policy, without which it is denied: the
			//    owner binds the export in the account.
			{Name: "binding in the account", Run: seedBinding(consumerOrg, consumerOwner, account)},
			// 5. After the binding, which brings the API into the
			//    account: the owner creates a Widget.
			{Name: "widget in the account", Run: seedWidget(consumerOrg, consumerOwner, account)},
		},

		// From the provider to the consumer, and from what is there to
		// who may use it.
		Validate: []Step{
			{Name: "workspace of the provider", Run: validateWorkspace(providersPath, providerWorkspace)},
			{Name: "export of the provider", Run: validateManifest(provider, "provider-export.yaml")},
			{Name: "policy of the export", Run: validatePolicy()},
			{Name: "organization", Run: validateOrganization(consumerOrg, consumerOwner)},
			{Name: "account", Run: validateAccount(org, consumerAccount)},
			{Name: "binding in the account", Run: validateBinding(account)},
			{Name: "widget in the account", Run: validateWidget(account)},
			{Name: "owner lists the widgets", Run: validateListsWidgets(consumerOrg, consumerOwner, account)},
			{Name: "owner lists the widgets through the gateway", Run: validateGatewayWidgets(consumerOrg, consumerOwner, account)},
			{Name: "guest is kept from the widgets", Run: validateNoWidgets(consumerOrg, consumerGuest, account)},
		},

		// ORDER MATTERS: the second Widget is created, read and deleted
		// again, so that Validate holds on a kept cluster.
		After: []Step{
			// The API bound on the release takes new objects, and
			// lets go of them.
			{Name: "second widget", Run: seedSecondWidget(consumerOrg, consumerOwner, account)},
			{Name: "delete the second widget", Run: deleteSecondWidget(consumerOrg, consumerOwner, account)},
		},
	}
}

// seedWorkspace creates the workspace of the manifest in the workspace at
// parent and waits for it to be Ready.
func seedWorkspace(parent, name, workspace string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := adminClient(ctx, env, parent)
		if err != nil {
			return err
		}
		if err := create(ctx, cl, name); err != nil {
			return err
		}
		return workspaceReady(ctx, env, parent, workspace)
	}
}

// validateWorkspace: the workspace in the workspace at parent is Ready.
func validateWorkspace(parent, workspace string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		return workspaceReady(ctx, env, parent, workspace)
	}
}

// seedPolicy creates the APIExportPolicy of the export of the provider and
// waits for it to be Ready: Platform Mesh then has written what lets the
// accounts of the organization bind the export.
func seedPolicy() stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := adminClient(ctx, env, tenant.OrgsPath)
		if err != nil {
			return err
		}
		if err := create(ctx, cl, "provider-policy.yaml"); err != nil {
			return err
		}
		return policyReady(ctx, cl)
	}
}

// validatePolicy: the APIExportPolicy is there and Ready.
func validatePolicy() stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := adminClient(ctx, env, tenant.OrgsPath)
		if err != nil {
			return err
		}
		return policyReady(ctx, cl)
	}
}

func policyReady(ctx context.Context, cl ctrlruntimeclient.Client) error {
	policies, err := existing(ctx, cl, "provider-policy.yaml")
	if err != nil {
		return err
	}
	return ready(policies[0])
}

// seedBinding binds the export of the provider in the workspace at path, as
// the user, logged in, and waits for the binding to be bound. That the user
// may bind is what the policy grants.
func seedBinding(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := userClient(ctx, env, org, email, path)
		if err != nil {
			return err
		}
		if err := create(ctx, cl, "consumer-binding.yaml"); err != nil {
			return err
		}
		return bindingBound(ctx, env, path)
	}
}

// validateBinding: the binding in the workspace at path is bound.
func validateBinding(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		return bindingBound(ctx, env, path)
	}
}

func bindingBound(ctx context.Context, env Environment, path string) error {
	cl, err := adminClient(ctx, env, path)
	if err != nil {
		return err
	}
	bindings, err := existing(ctx, cl, "consumer-binding.yaml")
	if err != nil {
		return err
	}
	if phase, _, _ := unstructured.NestedString(bindings[0].Object, "status", "phase"); phase != "Bound" {
		return fmt.Errorf("binding %s in %s is %q, not Bound", bindings[0].GetName(), path, phase)
	}
	return nil
}

// seedWidget creates the Widget of the manifest in the workspace at path,
// as the user, logged in.
func seedWidget(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := userClient(ctx, env, org, email, path)
		if err != nil {
			return err
		}
		return create(ctx, cl, "consumer-widget.yaml")
	}
}

// validateWidget: the Widget of the manifest is in the workspace at path,
// with the spec it was seeded with.
func validateWidget(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := adminClient(ctx, env, path)
		if err != nil {
			return err
		}
		want, err := manifest("consumer-widget.yaml")
		if err != nil {
			return err
		}
		got, err := existing(ctx, cl, "consumer-widget.yaml")
		if err != nil {
			return err
		}
		wantNote, _, _ := unstructured.NestedString(want[0].Object, "spec", "note")
		if note, _, _ := unstructured.NestedString(got[0].Object, "spec", "note"); note != wantNote {
			return steps.Terminal("widget %s in %s has the note %q, not %q", got[0].GetName(), path, note, wantNote)
		}
		return nil
	}
}

// listWidgets lists the Widgets in the workspace at path, as the user,
// logged in.
func listWidgets(ctx context.Context, env Environment, org, email, path string) ([]string, error) {
	cl, err := userClient(ctx, env, org, email, path)
	if err != nil {
		return nil, err
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(widgets.GroupVersion().WithKind(widgets.Kind + "List"))
	if err := cl.List(ctx, list); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.GetName())
	}
	return names, nil
}

// validateListsWidgets: the user, logged in, lists the Widgets in the
// workspace at path, and there is the one of the manifest.
func validateListsWidgets(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		names, err := listWidgets(ctx, env, org, email, path)
		if err != nil {
			return fmt.Errorf("%s listing the widgets in %s: %w", email, path, err)
		}
		return oneWidget(names, fmt.Sprintf("%s lists", email))
	}
}

// validateNoWidgets: the user, logged in, is not allowed to list the
// Widgets in the workspace at path.
func validateNoWidgets(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		_, err := listWidgets(ctx, env, org, email, path)
		return denied(err, fmt.Sprintf("%s listing the widgets in %s", email, path))
	}
}

// validateGatewayWidgets: the user, logged in, lists the Widgets in the
// workspace at path through the GraphQL gateway. The gateway serves them
// only if it has generated the schema of the workspace with the API of the
// provider in it.
func validateGatewayWidgets(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		token, err := login(ctx, env, org, email)
		if err != nil {
			return err
		}
		var answer struct {
			Group struct {
				V1alpha1 struct {
					Widgets struct {
						Items []struct{ Metadata struct{ Name string } }
					} `json:"Widgets"`
				} `json:"v1alpha1"`
			} `json:"seed_upgrade_platform_mesh_io"`
		}
		query := `{ seed_upgrade_platform_mesh_io { v1alpha1 { Widgets { items { metadata { name } } } } } }`
		if err := graphQL(ctx, env, org, gatewayPath(path), token, query, &answer); err != nil {
			return fmt.Errorf("the widgets of %s through the gateway: %w", path, err)
		}
		names := make([]string, 0, len(answer.Group.V1alpha1.Widgets.Items))
		for _, item := range answer.Group.V1alpha1.Widgets.Items {
			names = append(names, item.Metadata.Name)
		}
		return oneWidget(names, "the gateway lists")
	}
}

// oneWidget returns a terminal error unless names is the one Widget of the
// manifest.
func oneWidget(names []string, who string) error {
	want, err := manifest("consumer-widget.yaml")
	if err != nil {
		return err
	}
	if len(names) != 1 || names[0] != want[0].GetName() {
		return steps.Terminal("%s the widgets %v, not just %s", who, names, want[0].GetName())
	}
	return nil
}

// secondWidget is the Widget After creates: the one of the manifest under
// another name.
func secondWidget() (*unstructured.Unstructured, error) {
	objects, err := manifest("consumer-widget.yaml")
	if err != nil {
		return nil, err
	}
	objects[0].SetName(objects[0].GetName() + "-after")
	return objects[0], nil
}

// seedSecondWidget creates a second Widget in the workspace at path, as the
// user, logged in, and reads it back.
func seedSecondWidget(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := userClient(ctx, env, org, email, path)
		if err != nil {
			return err
		}
		widget, err := secondWidget()
		if err != nil {
			return err
		}
		key := ctrlruntimeclient.ObjectKeyFromObject(widget)
		if err := ctrlruntimeclient.IgnoreAlreadyExists(cl.Create(ctx, widget)); err != nil {
			return fmt.Errorf("%s creating widget %s in %s: %w", email, key, path, err)
		}
		return cl.Get(ctx, key, widget)
	}
}

// deleteSecondWidget deletes the Widget of seedSecondWidget, as the user,
// logged in, and waits until it is gone.
func deleteSecondWidget(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := userClient(ctx, env, org, email, path)
		if err != nil {
			return err
		}
		widget, err := secondWidget()
		if err != nil {
			return err
		}
		key := ctrlruntimeclient.ObjectKeyFromObject(widget)
		if err := ctrlruntimeclient.IgnoreNotFound(cl.Delete(ctx, widget)); err != nil {
			return fmt.Errorf("%s deleting widget %s in %s: %w", email, key, path, err)
		}
		return gone(cl.Get(ctx, key, widget), fmt.Sprintf("widget %s in %s", key, path))
	}
}
