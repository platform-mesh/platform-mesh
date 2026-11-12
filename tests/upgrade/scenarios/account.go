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
	"maps"

	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The steps of the tenant scenario (tenant.go) about the
// account of the organization and what the tenant keeps in its workspace.

// accountData is what the ConfigMap in the workspace of the account holds.
var accountData = map[string]string{"seeded-by": "tests/upgrade", "answer": "42"}

// seedAccount creates the account in the organization, as its owner.
func seedAccount(ctx context.Context, env Environment) error {
	kcp, err := env.KCP(ctx)
	if err != nil {
		return err
	}
	_, err = tenant.ApplyAccount(ctx, kcp, tenantOrg, tenantAccount, tenantOwner)
	return err
}

// seedAccountConfigMap creates a ConfigMap in the workspace of the account:
// plain data of the tenant, which no operator of Platform Mesh owns.
func seedAccountConfigMap(ctx context.Context, env Environment) error {
	cl, err := tenantClient(ctx, env, tenant.AccountPath(tenantOrg, tenantAccount), "")
	if err != nil {
		return err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceDefault, Name: tenantConfigMap},
		Data:       accountData,
	}
	if err := cl.Create(ctx, cm); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// validateAccount: the account is Ready and has its workspace.
func validateAccount(ctx context.Context, env Environment) error {
	kcp, err := env.KCP(ctx)
	if err != nil {
		return err
	}
	if _, err := tenant.GetAccount(ctx, kcp, tenantOrg, tenantAccount); err != nil {
		return err
	}
	return workspaceReady(ctx, kcp, tenant.OrganizationPath(tenantOrg), tenantAccount)
}

// validateAccountConfigMap: the ConfigMap is in the workspace of the
// account, with what seedAccountConfigMap put into it.
func validateAccountConfigMap(ctx context.Context, env Environment) error {
	cl, err := tenantClient(ctx, env, tenant.AccountPath(tenantOrg, tenantAccount), "")
	if err != nil {
		return err
	}
	cm := &corev1.ConfigMap{}
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: metav1.NamespaceDefault, Name: tenantConfigMap}, cm); err != nil {
		return err
	}
	if !maps.Equal(cm.Data, accountData) {
		return steps.Terminal("configmap %s holds %v, not %v", tenantConfigMap, cm.Data, accountData)
	}
	return nil
}
