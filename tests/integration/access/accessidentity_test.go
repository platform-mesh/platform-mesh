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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accessv1alpha1 "go.platform-mesh.io/sdk/access/apis/v1alpha1"
	accessctrl "go.platform-mesh.io/sdk/access/pkg/controller/access"
	identityctrl "go.platform-mesh.io/sdk/access/pkg/controller/accessidentity"
	"go.platform-mesh.io/platform-mesh/tests/framework/fixtures"
	"go.platform-mesh.io/platform-mesh/tests/framework/kcp"
	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	kcptc "github.com/kcp-dev/kcp-testcontainers"
	"github.com/kcp-dev/sdk/apis/core"

	_ "embed"
)

const (
	defaultTestTimeout  = 15 * time.Second
	defaultTickInterval = 250 * time.Millisecond

	// workspaceName is the kcp workspace the suite reconciles in. Fixed
	// so re-runs against the shared kcp reuse it.
	workspaceName = "access-sdk-integration"

	// testNamespace is where AccessIdentity + its Secret live.
	testNamespace = "default"

	accessIdentityCRDName = "accessidentities.access-sdk.platform-mesh.io"
	accessCRDName         = "accesses.access-sdk.platform-mesh.io"
)

//go:embed testdata/accessidentity-crd.yaml
var accessIdentityCRD []byte

//go:embed testdata/access-crd.yaml
var accessCRD []byte

// env is what the subtests share: a client into the test workspace and
// the admin REST config used to bootstrap SA tokens.
type env struct {
	cl       ctrlruntimeclient.Client
	adminCfg *rest.Config
}

func TestAccess(t *testing.T) {
	e := setup(t)

	t.Run("AccessIdentityMintsSecret", func(t *testing.T) { testAccessIdentityMintsSecret(t, e) })
	t.Run("AccessIdentityRotatesOnSecretDeletion", func(t *testing.T) { testAccessIdentityRotatesOnSecretDeletion(t, e) })
	t.Run("AccessRevokesExpiredToken", func(t *testing.T) { testAccessRevokesExpiredToken(t, e) })
	t.Run("AccessRenewsExpiringToken", func(t *testing.T) { testAccessRenewsExpiringToken(t, e) })
	t.Run("HandlerDeliversKubeconfig", func(t *testing.T) { testHandlerDeliversKubeconfig(t, e) })
}

// setup creates a kcp workspace, installs the AccessIdentity CRD, starts
// the identity controller in-process against that workspace and ensures
// the test namespace exists.
func setup(t *testing.T) *env {
	t.Helper()
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	kcpEnv, err := kcp.Default(t.Context())
	require.NoError(t, err)

	s := scheme.New(t, accessv1alpha1.AddToScheme, rbacv1.AddToScheme)

	wsPath := core.RootCluster.Path().Join(workspaceName)
	_, err = kcpEnv.CreateWorkspace(t.Context(), wsPath.String(), kcptc.WaitForReachable())
	require.NoError(t, err, "creating workspace %s", wsPath)

	cl, err := kcpEnv.Client(wsPath.String(), ctrlruntimeclient.Options{Scheme: s})
	require.NoError(t, err)

	for _, obj := range fixtures.Decode(t, accessIdentityCRD) {
		fixtures.Apply(t, cl, obj)
	}
	for _, obj := range fixtures.Decode(t, accessCRD) {
		fixtures.Apply(t, cl, obj)
	}
	wait.CRDEstablished(t, cl, accessIdentityCRDName)
	wait.CRDEstablished(t, cl, accessCRDName)

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
	fixtures.Apply(t, cl, ns)

	startControllers(t, kcpEnv, wsPath.String(), s)

	return &env{cl: cl, adminCfg: kcpEnv.RESTConfig(wsPath.String())}
}

// startControllers runs the AccessIdentity and Access reconcilers
// against the workspace at path, the way cmd/access-controller does.
func startControllers(t *testing.T, kcpEnv *kcptc.Instance, path string, s *runtime.Scheme) {
	t.Helper()
	mgr, err := ctrl.NewManager(kcpEnv.RESTConfig(path), ctrl.Options{
		Scheme:         s,
		Metrics:        metricsserver.Options{BindAddress: "0"},
		LeaderElection: false,
		BaseContext:    t.Context,
	})
	require.NoError(t, err)

	require.NoError(t, (&identityctrl.Reconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr))

	require.NoError(t, (&accessctrl.Reconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("manager exited with error: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Errorf("manager did not stop within 30s")
		}
	})

	synced := make(chan bool, 1)
	go func() { synced <- mgr.GetCache().WaitForCacheSync(ctx) }()
	select {
	case ok := <-synced:
		require.True(t, ok, "manager cache did not sync")
	case err := <-done:
		done <- err
		require.NoError(t, err, "manager exited before its cache synced")
	}
}

// testAccessIdentityMintsSecret: creating an AccessIdentity results in
// a Secret with the access token, status.tokenID set and Ready=True.
func testAccessIdentityMintsSecret(t *testing.T, e *env) {
	ctx := t.Context()
	name := "test-identity-mint"
	identity := &accessv1alpha1.AccessIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
	}
	require.NoError(t, e.cl.Create(ctx, identity))

	// Secret appears with token data and the token-id label.
	var tokenID string
	wait.Until(t, func() bool {
		secret := &corev1.Secret{}
		if err := e.cl.Get(ctx, types.NamespacedName{Name: name + "-access-token", Namespace: testNamespace}, secret); err != nil {
			return false
		}
		tokenID = secret.Labels[accessv1alpha1.TokenIDLabelKey]
		return tokenID != "" && len(secret.Data[accessv1alpha1.AccessTokenSecretKey]) > 0
	}, defaultTestTimeout, defaultTickInterval, "access-token Secret should be minted")

	// Status mirrors the Secret: tokenID set, Ready=True.
	updated := &accessv1alpha1.AccessIdentity{}
	wait.Until(t, func() bool {
		if err := e.cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: testNamespace}, updated); err != nil {
			return false
		}
		return updated.Status.TokenID == tokenID &&
			apimeta.IsStatusConditionTrue(updated.Status.Conditions, accessv1alpha1.AccessIdentityReady)
	}, defaultTestTimeout, defaultTickInterval, "AccessIdentity should be Ready with matching tokenID")

	cond := apimeta.FindStatusCondition(updated.Status.Conditions, accessv1alpha1.AccessIdentityReady)
	require.NotNil(t, cond)
	assert.Equal(t, accessv1alpha1.AccessIdentityReadyReason, cond.Reason)
}

// testAccessIdentityRotatesOnSecretDeletion: deleting the access-token
// Secret is how rotation is triggered. The next reconcile mints a fresh
// (tokenID, HMAC) pair and status.tokenID follows.
func testAccessIdentityRotatesOnSecretDeletion(t *testing.T, e *env) {
	ctx := t.Context()
	name := "test-identity-rotate"
	identity := &accessv1alpha1.AccessIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
	}
	require.NoError(t, e.cl.Create(ctx, identity))

	secretKey := types.NamespacedName{Name: name + "-access-token", Namespace: testNamespace}

	var firstTokenID string
	var firstToken []byte
	wait.Until(t, func() bool {
		secret := &corev1.Secret{}
		if err := e.cl.Get(ctx, secretKey, secret); err != nil {
			return false
		}
		firstTokenID = secret.Labels[accessv1alpha1.TokenIDLabelKey]
		firstToken = secret.Data[accessv1alpha1.AccessTokenSecretKey]
		return firstTokenID != "" && len(firstToken) > 0
	}, defaultTestTimeout, defaultTickInterval, "initial access-token Secret should be minted")

	// Delete the Secret to request rotation. The owner reference to the
	// AccessIdentity would garbage-collect it on identity deletion;
	// deleting it directly here does not touch the identity.
	toDelete := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretKey.Name, Namespace: secretKey.Namespace}}
	require.NoError(t, e.cl.Delete(ctx, toDelete))

	// A fresh Secret appears with a different tokenID and a different token.
	var secondTokenID string
	wait.Until(t, func() bool {
		secret := &corev1.Secret{}
		if err := e.cl.Get(ctx, secretKey, secret); err != nil {
			return false
		}
		secondTokenID = secret.Labels[accessv1alpha1.TokenIDLabelKey]
		if secondTokenID == "" || secondTokenID == firstTokenID {
			return false
		}
		newToken := secret.Data[accessv1alpha1.AccessTokenSecretKey]
		return len(newToken) > 0 && string(newToken) != string(firstToken)
	}, defaultTestTimeout, defaultTickInterval, "rotated access-token Secret should have a new tokenID")

	// Status catches up to the new tokenID and stays Ready.
	wait.Until(t, func() bool {
		cur := &accessv1alpha1.AccessIdentity{}
		if err := e.cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: testNamespace}, cur); err != nil {
			return false
		}
		return cur.Status.TokenID == secondTokenID &&
			apimeta.IsStatusConditionTrue(cur.Status.Conditions, accessv1alpha1.AccessIdentityReady)
	}, defaultTestTimeout, defaultTickInterval, "AccessIdentity status should track the rotated tokenID")
}
