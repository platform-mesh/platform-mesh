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

package access

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmaccessv1alpha1 "go.platform-mesh.io/apis/access/v1alpha1"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	sdkaccessv1alpha1 "go.platform-mesh.io/apis/sdk-access/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/pm"
	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

const (
	// remoteNamespace is where the sdk-access-* charts live in the kind
	// profile: AccessIdentity and the delivered Access objects live here.
	remoteNamespace = "platform-mesh-system"

	// accessServerURL points access-operator (running in the same
	// cluster) at the sdk-access-server Service. TLS is disabled in the
	// kind profile, so this is plain HTTP on the Service port.
	accessServerURL = "http://sdk-access-server.platform-mesh-system.svc:443/"

	// sdkAccessIdentityLabel mirrors
	// services/sdk-access-server/pkg/server/handler.go. We duplicate the
	// string to avoid an e2e dependency on server internals.
	sdkAccessIdentityLabel = "access-sdk.platform-mesh.io/identity"

	readinessTimeout = 2 * time.Minute
	workspaceTimeout = 2 * time.Minute
	pollInterval     = 2 * time.Second
)

// TestHandshakeHappyPath exercises the RFC 016 flow end-to-end: an
// AccessGrant in a fresh account workspace points at an AccessIdentity
// running on PM's own runtime cluster, access-operator completes the
// /access handshake, and sdk-access-server records the delivered
// kubeconfig as an Active Access.
func TestHandshakeHappyPath(t *testing.T) {
	ctx := t.Context()
	id := rand.String(8)
	org := "e2e-access-" + id
	account := "team"
	owner := fmt.Sprintf("owner-%s@example.com", id)
	t.Logf("organization %s, owner %s", org, owner)

	kcp := pm.KCP(t, "")
	kubeCfg := pm.Kube(t)
	kube := accessClient(t, kubeCfg)

	// 1. Fresh org + account. The organization goes on cleanup, taking
	// its workspace, the account below it, and anything we created in
	// them with it.
	orgs := tenant.Client(t, kcp, tenant.OrgsPath)
	t.Cleanup(func() {
		ctx := context.Background()
		if err := orgs.Delete(ctx, &pmcorev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: org}}); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("deleting organization %s: %v", org, err)
		}
	})

	tenant.EnsureOrganization(t, kcp, org, owner)
	wait.WorkspaceReadyWithin(t, orgs, org, workspaceTimeout, time.Second)
	tenant.EnsureAccount(t, kcp, org, account, owner)
	wait.WorkspaceReadyWithin(t, tenant.Client(t, kcp, tenant.OrganizationPath(org)), account, workspaceTimeout, time.Second)

	acctCfg := pm.Workspace(kcp, tenant.AccountPath(org, account))
	acctCl := accessClient(t, acctCfg)

	// 2. Bind access.platform-mesh.io in the account workspace so
	// AccessGrant resources are reachable there.
	binding := &kcpapisv1alpha2.APIBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "access.platform-mesh.io"},
		Spec: kcpapisv1alpha2.APIBindingSpec{
			Reference: kcpapisv1alpha2.BindingReference{
				Export: &kcpapisv1alpha2.ExportBindingReference{
					Path: "root:platform-mesh-system",
					Name: "access.platform-mesh.io",
				},
			},
		},
	}
	require.NoError(t, acctCl.Create(ctx, binding))
	wait.APIBindingBound(t, acctCl, binding.Name)

	// 3. Namespace + ServiceAccount in the account workspace, with a
	// Role granting self-TokenRequest so access-operator's renewal loop
	// can keep the delivered kubeconfig fresh.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "access-" + id}}
	require.NoError(t, acctCl.Create(ctx, ns))

	saName := "delivery"
	require.NoError(t, acctCl.Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: ns.Name},
	}))
	require.NoError(t, acctCl.Create(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "renew-self", Namespace: ns.Name},
		Rules: []rbacv1.PolicyRule{{
			APIGroups:     []string{""},
			Resources:     []string{"serviceaccounts/token"},
			ResourceNames: []string{saName},
			Verbs:         []string{"create"},
		}},
	}))
	require.NoError(t, acctCl.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "renew-self", Namespace: ns.Name},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: saName, Namespace: ns.Name}},
		RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "renew-self", APIGroup: "rbac.authorization.k8s.io"},
	}))

	// 4. AccessIdentity on the runtime cluster. sdk-access-operator
	// mints an `<name>-access-token` Secret with a pma1 token we can
	// hand to access-operator through the AccessGrant.
	identityName := "e2e-" + id
	identity := &sdkaccessv1alpha1.AccessIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: identityName, Namespace: remoteNamespace},
	}
	require.NoError(t, kube.Create(ctx, identity))
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), identity); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("deleting AccessIdentity %s: %v", identityName, err)
		}
	})

	tokenValue := waitForAccessToken(t, kube, identityName)

	// 5. Mirror the access token into a Secret in the SA namespace, as
	// AccessGrant.spec.accessTokenSecretRef expects it.
	tokenSecretName := "remote-token"
	require.NoError(t, acctCl.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: tokenSecretName, Namespace: ns.Name},
		Data:       map[string][]byte{sdkaccessv1alpha1.AccessTokenSecretKey: []byte(tokenValue)},
	}))

	// 6. AccessGrant drives the handshake. access-operator picks it up
	// through the APIExportEndpointSlice binding in root:platform-mesh-system.
	grant := &pmaccessv1alpha1.AccessGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "to-remote", Namespace: ns.Name},
		Spec: pmaccessv1alpha1.AccessGrantSpec{
			SARef:     corev1.LocalObjectReference{Name: saName},
			AccessURL: accessServerURL,
			AccessTokenSecretRef: pmaccessv1alpha1.AccessTokenSecretRef{
				Name: tokenSecretName,
				Key:  sdkaccessv1alpha1.AccessTokenSecretKey,
			},
		},
	}
	require.NoError(t, acctCl.Create(ctx, grant))

	// 7. HandshakeCompleted=True / HandshakeSucceeded is the terminal
	// happy-path signal on the PM side.
	wait.Eventually(t, func() (bool, string) {
		cur := &pmaccessv1alpha1.AccessGrant{}
		if err := acctCl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: grant.Name, Namespace: grant.Namespace}, cur); err != nil {
			return false, err.Error()
		}
		cond := meta.FindStatusCondition(cur.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionHandshakeCompleted)
		if cond == nil {
			return false, "HandshakeCompleted condition not set yet"
		}
		if cond.Status != metav1.ConditionTrue || cond.Reason != pmaccessv1alpha1.AccessGrantHandshakeSucceededReason {
			return false, fmt.Sprintf("HandshakeCompleted=%s reason=%s: %s", cond.Status, cond.Reason, cond.Message)
		}
		return true, ""
	}, readinessTimeout, pollInterval, "AccessGrant should reach HandshakeSucceeded")

	// 8. Remote side records exactly one Active Access tagged with the
	// identity, carrying the kubeconfig the server stored.
	var deliveredAccess sdkaccessv1alpha1.Access
	wait.Eventually(t, func() (bool, string) {
		var list sdkaccessv1alpha1.AccessList
		if err := kube.List(ctx, &list,
			ctrlruntimeclient.InNamespace(remoteNamespace),
			ctrlruntimeclient.MatchingLabels{sdkAccessIdentityLabel: identityName}); err != nil {
			return false, err.Error()
		}
		if len(list.Items) != 1 {
			return false, fmt.Sprintf("want 1 Access, got %d", len(list.Items))
		}
		acc := list.Items[0]
		if acc.Status.Phase != sdkaccessv1alpha1.AccessPhaseActive {
			return false, fmt.Sprintf("Access phase=%q", acc.Status.Phase)
		}
		if acc.Spec.KubeconfigSecretRef == nil {
			return false, "Access has no kubeconfigSecretRef"
		}
		deliveredAccess = acc
		return true, ""
	}, readinessTimeout, pollInterval, "sdk-access-server should record an Active Access")

	// The delivered kubeconfig Secret exists and is non-empty.
	kcSecret := &corev1.Secret{}
	require.NoError(t, kube.Get(ctx, ctrlruntimeclient.ObjectKey{
		Name: deliveredAccess.Spec.KubeconfigSecretRef.Name, Namespace: remoteNamespace,
	}, kcSecret))
	require.NotEmpty(t, kcSecret.Data["kubeconfig"], "delivered kubeconfig Secret is empty")
}

// accessClient builds a controller-runtime client over the framework
// scheme extended with the access and sdk-access types. pm.NewClient's
// shared scheme doesn't carry those groups.
func accessClient(t testing.TB, cfg *rest.Config) ctrlruntimeclient.Client {
	t.Helper()
	s := scheme.New(t,
		pmcorev1alpha1.AddToScheme,
		pmaccessv1alpha1.AddToScheme,
		sdkaccessv1alpha1.AddToScheme,
	)
	cfg = rest.CopyConfig(cfg)
	if cfg.Timeout == 0 {
		cfg.Timeout = time.Minute
	}
	if cfg.WarningHandler == nil && cfg.WarningHandlerWithContext == nil {
		cfg.WarningHandlerWithContext = rest.NoWarnings{}
	}
	cl, err := ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: s})
	require.NoError(t, err)
	return cl
}

// waitForAccessToken polls for the `<identity>-access-token` Secret the
// sdk-access-operator mints when an AccessIdentity becomes ready, and
// returns the pma1 token string it contains.
func waitForAccessToken(t testing.TB, cl ctrlruntimeclient.Client, identity string) string {
	t.Helper()
	var token string
	wait.Eventually(t, func() (bool, string) {
		sec := &corev1.Secret{}
		err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{
			Name: identity + "-access-token", Namespace: remoteNamespace,
		}, sec)
		if err != nil {
			return false, err.Error()
		}
		raw, ok := sec.Data[sdkaccessv1alpha1.AccessTokenSecretKey]
		if !ok || len(raw) == 0 {
			return false, "accessToken key not populated yet"
		}
		token = string(raw)
		return true, ""
	}, readinessTimeout, pollInterval, "sdk-access-operator should mint the access-token Secret")
	return token
}
