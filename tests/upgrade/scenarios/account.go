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

	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// Steps about an account and what its tenant keeps in its workspace. An
// account lives in the workspace of its parent, an organization or, when it
// is nested, another account; the steps take the path of that workspace.

const (
	// dataName names what seedData creates: a ConfigMap in the default
	// namespace, a namespace, and a ConfigMap and a Secret in that.
	dataName = "upgrade-seed"
)

// data is what the ConfigMaps and the Secret of seedData hold.
var data = map[string]string{"seeded-by": "tests/upgrade", "answer": "42"}

// seedAccount creates the account in the workspace at parent, as its owner.
func seedAccount(parent, name, owner string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kcp, err := env.KCP(ctx)
		if err != nil {
			return err
		}
		_, err = tenant.ApplyAccountIn(ctx, kcp, parent, name, owner)
		return err
	}
}

// seedData creates plain data of the tenant in the workspace at path, which
// no operator of Platform Mesh owns: a ConfigMap in the default namespace,
// and a namespace with a ConfigMap and a Secret.
func seedData(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := adminClient(ctx, env, path)
		if err != nil {
			return err
		}
		for _, obj := range dataObjects() {
			if err := cl.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("creating %T %s in %s: %w", obj, ctrlruntimeclient.ObjectKeyFromObject(obj), path, err)
			}
		}
		return nil
	}
}

// validateAccount: the account in the workspace at parent is Ready and has
// its workspace.
func validateAccount(parent, name string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kcp, err := env.KCP(ctx)
		if err != nil {
			return err
		}
		if _, err := tenant.GetAccountIn(ctx, kcp, parent, name); err != nil {
			return err
		}
		return workspaceReady(ctx, env, parent, name)
	}
}

// validateData: the data of seedData is in the workspace at path, as it was
// seeded. Keys added since, as by changeData, are none of its business.
func validateData(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := adminClient(ctx, env, path)
		if err != nil {
			return err
		}
		for _, want := range dataObjects() {
			key := ctrlruntimeclient.ObjectKeyFromObject(want)
			switch want := want.(type) {
			case *corev1.Namespace:
				got := &corev1.Namespace{}
				if err := cl.Get(ctx, key, got); err != nil {
					return err
				}
				if got.Status.Phase != corev1.NamespaceActive {
					return steps.Terminal("namespace %s in %s is %s", key.Name, path, got.Status.Phase)
				}
			case *corev1.ConfigMap:
				got := &corev1.ConfigMap{}
				if err := cl.Get(ctx, key, got); err != nil {
					return err
				}
				if !holds(got.Data, want.Data) {
					return steps.Terminal("configmap %s in %s holds %v, not %v", key, path, got.Data, want.Data)
				}
			case *corev1.Secret:
				got := &corev1.Secret{}
				if err := cl.Get(ctx, key, got); err != nil {
					return err
				}
				held := make(map[string]string, len(got.Data))
				for k, v := range got.Data {
					held[k] = string(v)
				}
				if !holds(held, want.StringData) {
					return steps.Terminal("secret %s in %s does not hold what was seeded (keys %v)", key, path, slices.Sorted(maps.Keys(held)))
				}
			}
		}
		return nil
	}
}

// changeData changes the ConfigMap of seedData in the default namespace of
// the workspace at path, and reads the change back: what the release stored
// can still be written.
func changeData(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := adminClient(ctx, env, path)
		if err != nil {
			return err
		}
		cm := &corev1.ConfigMap{}
		key := ctrlruntimeclient.ObjectKey{Namespace: metav1.NamespaceDefault, Name: dataName}
		if err := cl.Get(ctx, key, cm); err != nil {
			return err
		}
		if cm.Data["changed-after"] != "upgrade" {
			cm.Data["changed-after"] = "upgrade"
			if err := cl.Update(ctx, cm); err != nil {
				return fmt.Errorf("updating configmap %s in %s: %w", key, path, err)
			}
		}
		return nil
	}
}

// deleteAccount deletes the account in the workspace at parent and waits
// until it is gone, with its workspace.
func deleteAccount(parent, name string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		return deleteAccountIn(ctx, env, parent, name)
	}
}

// dataObjects are the objects of seedData, the namespace before what is in
// it.
func dataObjects() []ctrlruntimeclient.Object {
	return []ctrlruntimeclient.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceDefault, Name: dataName},
			Data:       maps.Clone(data),
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: dataName},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: dataName, Name: dataName},
			Data:       maps.Clone(data),
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: dataName, Name: dataName},
			StringData: maps.Clone(data),
		},
	}
}

// holds tells whether got has every key of want, with its value.
func holds(got, want map[string]string) bool {
	for k, v := range want {
		if held, ok := got[k]; !ok || held != v {
			return false
		}
	}
	return true
}

func newWorkspace() *kcptenancyv1alpha1.Workspace { return &kcptenancyv1alpha1.Workspace{} }

// workspaceReady returns an error unless the Workspace name in the
// workspace at path is Ready.
func workspaceReady(ctx context.Context, env Environment, path, name string) error {
	cl, err := adminClient(ctx, env, path)
	if err != nil {
		return err
	}
	ws := newWorkspace()
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, ws); err != nil {
		return err
	}
	if ws.Status.Phase != "Ready" {
		return fmt.Errorf("workspace %s in %s is %s", name, path, ws.Status.Phase)
	}
	return nil
}
