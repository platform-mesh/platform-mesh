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
	"maps"
	"slices"
	"strings"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Steps that pin what an upgrade must not change. That an object is there
// after the upgrade does not say that it is the same object: one that was
// deleted and created again looks alike and has lost what hung on the old
// one. The roles of an organization, for example, are stored under the ID
// of its logical cluster and in its authorization store; with a new cluster
// or a new store they are all gone, while every object still reads Ready.
//
// So seedIdentity remembers the identities of a scenario on the release
// (state.go), and validateIdentity compares.

// An identity reads identities of the installation, by a name each.
type identity func(ctx context.Context, env Environment) (map[string]string, error)

// seedIdentity remembers the identities under name. What is remembered
// already stays, so on a kept cluster the identities of the first run are
// the ones compared with.
func seedIdentity(name string, of ...identity) stepFunc {
	return func(ctx context.Context, env Environment) error {
		found, err := identities(ctx, env, of)
		if err != nil {
			return err
		}
		return remember(ctx, env, name, found)
	}
}

// validateIdentity: the identities are the ones seedIdentity remembered
// under name.
func validateIdentity(name string, of ...identity) stepFunc {
	return func(ctx context.Context, env Environment) error {
		want, err := recall(ctx, env, name)
		if err != nil {
			return err
		}
		got, err := identities(ctx, env, of)
		if err != nil {
			return err
		}
		var changed []string
		for _, key := range slices.Sorted(maps.Keys(want)) {
			if got[key] != want[key] {
				changed = append(changed, fmt.Sprintf("%s was %q and is %q", key, want[key], got[key]))
			}
		}
		if len(changed) != 0 {
			return steps.Terminal("not what was seeded, but replaced: %s", strings.Join(changed, "; "))
		}
		return nil
	}
}

// forgetIdentity drops what seedIdentity remembered under name, for a
// scenario that deletes what it seeded: the next Seed creates it anew.
func forgetIdentity(name string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		return forget(ctx, env, name)
	}
}

func identities(ctx context.Context, env Environment, of []identity) (map[string]string, error) {
	all := map[string]string{}
	for _, read := range of {
		found, err := read(ctx, env)
		if err != nil {
			return nil, err
		}
		for key, value := range found {
			if value == "" {
				return nil, fmt.Errorf("%s is not set yet", key)
			}
			all[key] = value
		}
	}
	return all, nil
}

// accountIdentity is the identity of the account, or organization, name in
// the workspace at parent: the Account object, its workspace, and the
// logical cluster behind the workspace.
func accountIdentity(parent, name string) identity {
	return func(ctx context.Context, env Environment) (map[string]string, error) {
		cl, err := adminClient(ctx, env, parent)
		if err != nil {
			return nil, err
		}
		key := ctrlruntimeclient.ObjectKey{Name: name}
		account := &pmcorev1alpha1.Account{}
		if err := cl.Get(ctx, key, account); err != nil {
			return nil, fmt.Errorf("account %s in %s: %w", name, parent, err)
		}
		ws := newWorkspace()
		if err := cl.Get(ctx, key, ws); err != nil {
			return nil, fmt.Errorf("workspace %s in %s: %w", name, parent, err)
		}
		at := parent + ":" + name
		return map[string]string{
			"account " + at + " uid":       string(account.UID),
			"workspace " + at + " uid":     string(ws.UID),
			"workspace " + at + " cluster": ws.Spec.Cluster,
		}, nil
	}
}

// organizationIdentity is what the users and roles of the organization hang
// on, beyond accountIdentity: its authorization store, and the OIDC client
// that kubeconfigs handed out to its users name.
func organizationIdentity(org string) identity {
	return func(ctx context.Context, env Environment) (map[string]string, error) {
		cl, err := adminClient(ctx, env, tenant.OrgsPath)
		if err != nil {
			return nil, err
		}
		key := ctrlruntimeclient.ObjectKey{Name: org}
		store := &pmcorev1alpha1.Store{}
		if err := cl.Get(ctx, key, store); err != nil {
			return nil, fmt.Errorf("store %s: %w", org, err)
		}
		idp := &pmcorev1alpha1.IdentityProviderConfiguration{}
		if err := cl.Get(ctx, key, idp); err != nil {
			return nil, fmt.Errorf("identity provider configuration %s: %w", org, err)
		}
		return map[string]string{
			"store " + org + " id":             store.Status.StoreID,
			"client kubectl of " + org + " id": idp.Status.ManagedClients[kubectlClient].ClientID,
		}, nil
	}
}

// dataIdentity is the identity of the ConfigMap of seedData in the default
// namespace of the workspace at path.
func dataIdentity(path string) identity {
	return func(ctx context.Context, env Environment) (map[string]string, error) {
		cl, err := adminClient(ctx, env, path)
		if err != nil {
			return nil, err
		}
		cm := &corev1.ConfigMap{}
		if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: metav1.NamespaceDefault, Name: dataName}, cm); err != nil {
			return nil, err
		}
		return map[string]string{"configmap " + dataName + " in " + path + " uid": string(cm.UID)}, nil
	}
}
