//go:build e2e

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

package operator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/tests/framework/wait"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	managedProviderName   = "my-managed-provider"
	managedProviderSecret = managedProviderName + "-provider-kubeconfig"
	lifecycleTimeout      = 4 * time.Minute
)

// TestManagedProviderLifecycle life-cycles a ManagedProvider twice, checking
// spec.cleanupOnDelete: each time the operator creates a Provider in
// :root:providers:system, a provider workspace and a kubeconfig scoped to it
// on both the kcp and the runtime side. With cleanupOnDelete=false the kcp
// side survives deletion, with cleanupOnDelete=true everything goes.
//
// The runtime side deploys two Flux components (an OCI chart and a classic
// Helm repository chart) and waits for their Deployments, so the scenario
// needs Flux on the runtime cluster. The ManagedProvider CRD requires
// runtimeDeployments and the deploy finalizer cannot complete without the
// Flux kinds, so without Flux the test is skipped (the Tilt cluster has none).
func TestManagedProviderLifecycle(t *testing.T) {
	cl := kubeClient(t)
	requireFlux(t, cl)
	waitPlatformMeshReady(t, cl)

	providersWs := kcpClient(t, "root:providers")
	systemWs := kcpClient(t, "root:providers:system")
	system := &kcptenancyv1alpha1.Workspace{}
	require.NoError(t, providersWs.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "system"}, system), "getting workspace system in :root:providers")
	require.NotEmpty(t, system.Spec.Cluster, "cluster name of :root:providers:system")
	providerWsName := managedProviderName + "-" + system.Spec.Cluster
	key := ctrlruntimeclient.ObjectKey{Namespace: platformMeshNamespace, Name: managedProviderName}

	t.Cleanup(func() {
		err := cl.Delete(context.Background(), &pmprovidersv1alpha1.ManagedProvider{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name}})
		if err != nil && !apierrors.IsNotFound(err) {
			t.Logf("cleanup: deleting ManagedProvider: %v", err)
		}
	})

	t.Run("cleanupOnDelete=false keeps the kcp side", func(t *testing.T) {
		createManagedProviderAndValidate(t, cl, providersWs, systemWs, providerWsName, false)
		deleteManagedProvider(t, cl, key)

		provider := &pmprovidersv1alpha1.Provider{}
		require.NoError(t, systemWs.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: managedProviderName}, provider), "Provider in :root:providers:system should survive")
		require.Equal(t, pmprovidersv1alpha1.ProviderPhaseReady, provider.Status.Phase, "Provider should still be Ready")
		require.Nil(t, provider.DeletionTimestamp, "Provider should not be marked for deletion")
	})

	t.Run("cleanupOnDelete=true removes everything", func(t *testing.T) {
		createManagedProviderAndValidate(t, cl, providersWs, systemWs, providerWsName, true)
		deleteManagedProvider(t, cl, key)

		waitGone(t, providersWs, &kcptenancyv1alpha1.Workspace{}, ctrlruntimeclient.ObjectKey{Name: providerWsName}, 6*time.Minute)
		waitGone(t, systemWs, &pmprovidersv1alpha1.Provider{}, ctrlruntimeclient.ObjectKey{Name: managedProviderName}, lifecycleTimeout)
		for _, name := range runtimeDeploymentNames {
			waitGone(t, cl, &appsv1.Deployment{}, ctrlruntimeclient.ObjectKey{Namespace: platformMeshNamespace, Name: name}, lifecycleTimeout)
		}
	})
}

// runtimeDeploymentNames are the Deployments the two Flux components create.
var runtimeDeploymentNames = []string{"example-httpbin-operator", "podinfo"}

// requireFlux skips t unless the Flux kinds the ManagedProvider deploy
// subroutine creates are served by the runtime cluster.
func requireFlux(t *testing.T, cl ctrlruntimeclient.Client) {
	t.Helper()
	for _, crd := range []string{
		"helmreleases.helm.toolkit.fluxcd.io",
		"ocirepositories.source.toolkit.fluxcd.io",
		"helmrepositories.source.toolkit.fluxcd.io",
	} {
		err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: crd}, &apiextensionsv1.CustomResourceDefinition{})
		if apierrors.IsNotFound(err) {
			t.Skipf("Flux is not installed on the runtime cluster (no CRD %s); ManagedProvider runtimeDeployments need it", crd)
		}
		require.NoError(t, err, "getting CRD %s", crd)
	}
}

func createManagedProviderAndValidate(t *testing.T, cl, providersWs, systemWs ctrlruntimeclient.Client, providerWsName string, cleanupOnDelete bool) {
	t.Helper()
	key := ctrlruntimeclient.ObjectKey{Namespace: platformMeshNamespace, Name: managedProviderName}
	mp := &pmprovidersv1alpha1.ManagedProvider{
		// ManagedProvider is always co-located with the PlatformMesh object.
		ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
		Spec: pmprovidersv1alpha1.ManagedProviderSpec{
			CleanupOnDelete: cleanupOnDelete,
			RuntimeDeployments: []pmprovidersv1alpha1.ProviderComponentSpec{
				{
					// type=oci (default): chart packaged as an OCI artifact -> Flux OCIRepository.
					Flux: &pmprovidersv1alpha1.FluxComponentSpec{
						Chart:    "example-httpbin-operator",
						Registry: "ghcr.io/platform-mesh/helm-charts",
						Version:  "0.5.14",
					},
				},
				{
					// type=helm: chart from a classic HTTP Helm repository -> Flux HelmRepository.
					Flux: &pmprovidersv1alpha1.FluxComponentSpec{
						Type:     pmprovidersv1alpha1.FluxSourceTypeHelm,
						Chart:    "podinfo",
						Registry: "https://stefanprodan.github.io/podinfo",
						Version:  "6.7.1",
					},
				},
			},
			PlatformMeshReference: pmprovidersv1alpha1.PlatformMeshReferenceSpec{Name: platformMeshName},
		},
	}
	require.NoError(t, cl.Create(t.Context(), mp), "creating ManagedProvider")

	// kcp side: Provider in :root:providers:system, workspace
	// :root:providers:<name>-<system cluster>, Provider Ready with its kubeconfig secret.
	wait.Eventually(t, func() (bool, string) {
		if err := providersWs.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: providerWsName}, &kcptenancyv1alpha1.Workspace{}); err != nil {
			return false, fmt.Sprintf("getting workspace %s: %v", providerWsName, err)
		}
		return true, ""
	}, lifecycleTimeout, pollInterval, "provider workspace root:providers:%s should be created", providerWsName)

	provider := &pmprovidersv1alpha1.Provider{}
	wait.Eventually(t, func() (bool, string) {
		if err := systemWs.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: managedProviderName}, provider); err != nil {
			return false, fmt.Sprintf("getting Provider: %v", err)
		}
		if provider.Status.Phase != pmprovidersv1alpha1.ProviderPhaseReady {
			return false, fmt.Sprintf("phase %q", provider.Status.Phase)
		}
		return true, ""
	}, lifecycleTimeout, pollInterval, "Provider %s in :root:providers:system should be Ready", managedProviderName)
	require.Equal(t, &corev1.SecretReference{Name: managedProviderSecret, Namespace: platformMeshNamespace},
		provider.Status.ProviderKubeconfigSecretRef, "Provider status.providerKubeconfigSecretRef")
	listConfigMapsWithSecretKubeconfig(t, systemWs, provider.Status.ProviderKubeconfigSecretRef, "root:providers:"+providerWsName, "kcp")

	// runtime side: ManagedProvider gets the kubeconfig copied and reaches Ready.
	wait.Eventually(t, func() (bool, string) {
		if err := cl.Get(t.Context(), key, mp); err != nil {
			return false, fmt.Sprintf("getting ManagedProvider: %v", err)
		}
		if mp.Status.ProviderKubeconfigSecretRef == nil {
			return false, fmt.Sprintf("no providerKubeconfigSecretRef yet, phase %q", mp.Status.Phase)
		}
		return true, ""
	}, lifecycleTimeout, pollInterval, "ManagedProvider should get status.providerKubeconfigSecretRef")
	require.Equal(t, managedProviderSecret, mp.Status.ProviderKubeconfigSecretRef.Name)
	require.Equal(t, key.Namespace, mp.Status.ProviderKubeconfigSecretRef.Namespace)
	listConfigMapsWithSecretKubeconfig(t, cl, mp.Status.ProviderKubeconfigSecretRef, "root:providers:"+providerWsName, "runtime")

	wait.Eventually(t, func() (bool, string) {
		if err := cl.Get(t.Context(), key, mp); err != nil {
			return false, fmt.Sprintf("getting ManagedProvider: %v", err)
		}
		if mp.Status.Phase != pmprovidersv1alpha1.ManagedProviderPhaseReady {
			return false, fmt.Sprintf("phase %q", mp.Status.Phase)
		}
		return true, ""
	}, lifecycleTimeout, pollInterval, "ManagedProvider should reach phase Ready")
	for _, name := range runtimeDeploymentNames {
		wait.Eventually(t, func() (bool, string) {
			if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Namespace: key.Namespace, Name: name}, &appsv1.Deployment{}); err != nil {
				return false, fmt.Sprintf("getting Deployment: %v", err)
			}
			return true, ""
		}, lifecycleTimeout, pollInterval, "runtime Deployment %s should exist", name)
	}
}

// listConfigMapsWithSecretKubeconfig uses the provider kubeconfig in ref
// (read through cl) to list ConfigMaps in the provider workspace; there is at
// least kube-root-ca.crt.
func listConfigMapsWithSecretKubeconfig(t *testing.T, cl ctrlruntimeclient.Client, ref *corev1.SecretReference, providerWsPath, side string) {
	t.Helper()
	secret := &corev1.Secret{}
	require.NoError(t, cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, secret), "getting provider kubeconfig secret on the %s side", side)
	kubeconfig := secret.Data["kubeconfig"]
	require.NotEmpty(t, kubeconfig, "provider kubeconfig secret on the %s side has no kubeconfig", side)

	cms := &corev1.ConfigMapList{}
	require.NoError(t, kubeconfigClient(t, kubeconfig, providerWsPath).List(t.Context(), cms, ctrlruntimeclient.InNamespace("default")),
		"listing ConfigMaps with the provider kubeconfig from the %s side", side)
	require.NotEmpty(t, cms.Items, "listing ConfigMaps with the provider kubeconfig from the %s side should return kube-root-ca.crt at least", side)
}

func deleteManagedProvider(t *testing.T, cl ctrlruntimeclient.Client, key ctrlruntimeclient.ObjectKey) {
	t.Helper()
	require.NoError(t, cl.Delete(t.Context(), &pmprovidersv1alpha1.ManagedProvider{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name}}), "deleting ManagedProvider")
	waitGone(t, cl, &pmprovidersv1alpha1.ManagedProvider{}, key, lifecycleTimeout)
}

func waitGone(t *testing.T, cl ctrlruntimeclient.Client, obj ctrlruntimeclient.Object, key ctrlruntimeclient.ObjectKey, timeout time.Duration) {
	t.Helper()
	wait.Eventually(t, func() (bool, string) {
		err := cl.Get(t.Context(), key, obj)
		if apierrors.IsNotFound(err) {
			return true, ""
		}
		return false, fmt.Sprintf("still there (err=%v)", err)
	}, timeout, pollInterval, "%T %s should be deleted", obj, key)
}
