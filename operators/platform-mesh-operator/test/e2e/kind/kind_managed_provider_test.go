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

package e2e

import (
	"context"
	"time"

	"github.com/creasty/defaults"

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	providerscontroller "go.platform-mesh.io/platform-mesh-operator/internal/controller/providers"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmultiprovider "sigs.k8s.io/multicluster-runtime/providers/multi"

	mcapiexportprovider "github.com/kcp-dev/multicluster-provider/apiexport"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

func (s *KindTestSuite) TestManagedProvider01Bootstrap() {
	// Run the Providers operator
	s.logger.Info().Msg("starting Providers operator...")
	s.runProviderOperator()
}

func (s *KindTestSuite) TestManagedProvider02Lifecycle() {
	ctx := s.T().Context()

	s.Run("Ensure life-cycling ManagedProvider works", func() {
		// This test life-cycles ManagedProvider twice, validating ManagedProvider.spec.cleanupOnDelete.
		// In both cases, ManagedProvider is expected to create a Deployment in the runtime cluster,
		// and a kubeconfig scoped to provider's workspace.

		allProvidersScopedAdminClient := s.kcpClientForWorkspaceWithScheme(ctx, s.scheme, "root:providers")
		var systemProviderWs kcptenancyv1alpha1.Workspace
		err := allProvidersScopedAdminClient.Get(ctx, types.NamespacedName{Name: "system"}, &systemProviderWs)
		s.Require().NoError(err, "getting Workspace system in :root:providers should succeed")
		s.Require().NotEmpty(systemProviderWs.Spec.Cluster, "cluster name for :root:providers:system workspace should not be empty")
		providerWsName := "my-managed-provider-" + systemProviderWs.Spec.Cluster
		providerWsPath := "root:providers:" + providerWsName

		// First variant, with cleanupOnDelete=false. Only runtime resources are expected to be deleted on ManagedProvider deletion.
		waitForManagedProviderAndValidate(ctx, s, func(mp *pmprovidersv1alpha1.ManagedProvider) {
			mp.Spec.CleanupOnDelete = false
		})
		// Deleting ManagedProvider should NOT delete its artifacts on kcp side since spec.cleanupOnDelete=false.
		// The Deployment should be deleted though.
		s.logger.Info().Msgf("Deleting ManagedProvider with cleanupOnDelete=false")
		managedProvider := pmprovidersv1alpha1.ManagedProvider{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "platform-mesh-system", // ManagedProvider is always co-located with PlatformMesh object namespace.
				Name:      "my-managed-provider",
			},
		}
		err = s.client.Delete(ctx, &managedProvider)
		s.Require().NoError(err, "deleting ManagedProvider should succeed")
		s.Require().Eventually(func() bool {
			err = s.client.Get(ctx, types.NamespacedName{
				Namespace: "platform-mesh-system",
				Name:      "my-managed-provider",
			}, &managedProvider)
			return apierrors.IsNotFound(err)
		}, 240*time.Second, 5*time.Second, "waiting for ManagedProvider to be deleted, but has err=%q", err)

		var provider pmprovidersv1alpha1.Provider
		systemProviderWsClient := s.kcpClientForWorkspaceWithScheme(ctx, s.scheme, "root:providers:system")
		err = systemProviderWsClient.Get(ctx, types.NamespacedName{
			Name: "my-managed-provider",
		}, &provider)
		s.Require().NoError(err, "getting Provider with scopedKcpAdminClient should succeed")
		s.Require().Equal("Ready", provider.Status.Phase, "Provider on kcp side should have reached Phase=Ready")
		s.Require().Nil(provider.DeletionTimestamp, "Provider should not be marked for deletion")

		// Second variant, with cleanupOnDelete=true. Everything is expected to be gone once ManagedProvider is deleted.

		s.logger.Info().Msgf("Re-creating ManagedProvider with cleanupOnDelete=true")
		waitForManagedProviderAndValidate(ctx, s, func(mp *pmprovidersv1alpha1.ManagedProvider) {
			mp.Spec.CleanupOnDelete = true
		})
		s.logger.Info().Msgf("Deleting ManagedProvider with cleanupOnDelete=true")
		err = s.client.Delete(ctx, &pmprovidersv1alpha1.ManagedProvider{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "platform-mesh-system",
				Name:      "my-managed-provider",
			},
		})
		s.Require().NoError(err, "deleting ManagedProvider should succeed")

		s.logger.Info().Msgf("Waiting until workspace %s is deleted", providerWsPath)
		var ws kcptenancyv1alpha1.Workspace
		s.Require().Eventually(func() bool {
			err = allProvidersScopedAdminClient.Get(ctx, types.NamespacedName{
				Name: providerWsName,
			}, &ws)
			return apierrors.IsNotFound(err)
		}, 6*time.Minute, 5*time.Second, "waiting for workspace to be deleted, but has err=%v, Workspace=%#v", err, ws) // This may take a long time...

		s.logger.Info().Msgf("Waiting until Provider my-managed-provider in root:providers:system is deleted")
		s.Require().Eventually(func() bool {
			err = systemProviderWsClient.Get(ctx, types.NamespacedName{
				Name: "my-managed-provider",
			}, &pmprovidersv1alpha1.Provider{})
			return apierrors.IsNotFound(err)
		}, 240*time.Second, 5*time.Second, "waiting for Provider to be deleted, but has err=%v", err)

		s.logger.Info().Msgf("Waiting until Deployment example-httpbin-operator is deleted (type=oci component)")
		var deployment appsv1.Deployment
		s.Require().Eventually(func() bool {
			err = s.client.Get(ctx, types.NamespacedName{
				Namespace: "platform-mesh-system",
				Name:      "example-httpbin-operator",
			}, &deployment)
			return apierrors.IsNotFound(err)
		}, 240*time.Second, 5*time.Second, "waiting for Deployment to be deleted, but has err=%v, Deployment=%#v", err, deployment)

		s.logger.Info().Msgf("Waiting until Deployment podinfo is deleted (type=helm component)")
		var podinfoDeployment appsv1.Deployment
		s.Require().Eventually(func() bool {
			err = s.client.Get(ctx, types.NamespacedName{
				Namespace: "platform-mesh-system",
				Name:      "podinfo",
			}, &podinfoDeployment)
			return apierrors.IsNotFound(err)
		}, 240*time.Second, 5*time.Second, "waiting for podinfo Deployment to be deleted, but has err=%v, Deployment=%#v", err, podinfoDeployment)

		s.logger.Info().Msgf("Waiting until ManagedProvider my-managed-provider is deleted")
		s.Require().Eventually(func() bool {
			err = s.client.Get(ctx, types.NamespacedName{
				Namespace: "platform-mesh-system",
				Name:      "my-managed-provider",
			}, &managedProvider)
			return apierrors.IsNotFound(err)
		}, 240*time.Second, 5*time.Second, "waiting for ManagedProvider to be deleted, but has err=%v, ManagedProvider=%#v", err, managedProvider)
	})
}

func waitForManagedProviderAndValidate(ctx context.Context, s *KindTestSuite, patchManagedProviderCreate func(*pmprovidersv1alpha1.ManagedProvider)) {
	managedProviderName := types.NamespacedName{
		Namespace: "platform-mesh-system",
		Name:      "my-managed-provider",
	}
	managedProvider := pmprovidersv1alpha1.ManagedProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedProviderName.Name,
			Namespace: managedProviderName.Namespace,
		},
		Spec: pmprovidersv1alpha1.ManagedProviderSpec{
			RuntimeDeployments: []pmprovidersv1alpha1.ProviderComponentSpec{
				{
					// type=oci (default): chart packaged as an OCI artifact → Flux OCIRepository.
					Flux: &pmprovidersv1alpha1.FluxComponentSpec{
						Chart:    "example-httpbin-operator",
						Registry: "ghcr.io/platform-mesh/helm-charts",
						Version:  "0.5.14",
					},
				},
				{
					// type=helm: chart from a classic HTTP Helm repository → Flux HelmRepository.
					Flux: &pmprovidersv1alpha1.FluxComponentSpec{
						Type:     pmprovidersv1alpha1.FluxSourceTypeHelm,
						Chart:    "podinfo",
						Registry: "https://stefanprodan.github.io/podinfo",
						Version:  "6.7.1",
					},
				},
			},
			PlatformMeshReference: pmprovidersv1alpha1.PlatformMeshReferenceSpec{
				Name: "platform-mesh",
			},
		},
	}
	patchManagedProviderCreate(&managedProvider)

	createProviderClientFromSecretRefAndListConfigMaps := func(cl ctrlruntimeclient.Client, secretName types.NamespacedName, who string) {
		s.logger.Info().Msgf("Getting provider kubeconfig secret on %s side", who)
		var kubeconfigSecret corev1.Secret
		err := cl.Get(ctx, secretName, &kubeconfigSecret)
		s.Require().NoError(err, "getting kubeconfig secret from %s should succeed", who)
		kubeconfig := kubeconfigSecret.Data["kubeconfig"]
		s.Require().NotEmpty(kubeconfig, "kubeconfig not set in secret on %s side", who)

		// List ConfigMaps using the provider kubeconfig just to see if it works.
		providerClient, _, err := createKubernetesClient(kubeconfig, s.scheme)
		s.Require().NoError(err, "creating client from provider kubeconfig from %s side should succeed", who)
		s.logger.Info().Msgf("Listing ConfigMaps in provider workspace using kubeconfig from %s side", who)
		cmList := &corev1.ConfigMapList{}
		err = providerClient.List(ctx, cmList, ctrlruntimeclient.InNamespace("default"))
		s.Require().NoError(err, "listing ConfigMaps in provider workspace using scoped kubeconfig from %s side should succeed", who)
		s.Require().Greater(len(cmList.Items), 0,
			"listing ConfigMap in provider workspace using scoped kubeconfig from %s side should return non-zero items", who) // Should contain kube-root-ca.crt CM at least.
	}

	allProvidersScopedAdminClient := s.kcpClientForWorkspaceWithScheme(ctx, s.scheme, "root:providers")

	s.logger.Info().Msgf("Creating ManagedProvider %q", managedProvider.Name)
	err := s.client.Create(ctx, &managedProvider)
	s.Require().NoError(err, "creating ManagedProvider should succeed")
	s.logger.Info().Msgf("ManagedProvider %q created", managedProvider.Name)

	// Validate kcp side first.

	// We're expecting the Providers controller to build a workspace :root:providers:<ManagedProvider.Name>-<Provider's logical cluster name>.
	// Let's build the name first.
	var systemProviderWs kcptenancyv1alpha1.Workspace
	err = allProvidersScopedAdminClient.Get(ctx, types.NamespacedName{Name: "system"}, &systemProviderWs)
	s.Require().NoError(err, "getting Workspace system in :root:providers should succeed")
	s.Require().NotEmpty(systemProviderWs.Spec.Cluster, "cluster name for :root:providers:system workspace should not be empty")

	s.logger.Info().Msgf("Waiting until Provider is created in root:providers:system")
	systemProviderWsClient := s.kcpClientForWorkspaceWithScheme(ctx, s.scheme, "root:providers:system")
	s.Require().Eventually(func() bool {
		err = systemProviderWsClient.Get(ctx, types.NamespacedName{
			Name: managedProvider.Name,
		}, &pmprovidersv1alpha1.Provider{})
		return err == nil
	}, 240*time.Second, 5*time.Second, "waiting for Provider in :root:providers:system to be created, but has err=%v", err)

	providerWsName := managedProvider.Name + "-" + systemProviderWs.Spec.Cluster
	providerWsPath := "root:providers:" + providerWsName
	s.logger.Info().Msgf("Provider contents should be in %s workspace", providerWsPath)

	s.logger.Info().Msgf("Waiting until %s workspace is created", providerWsPath)
	s.Require().Eventually(func() bool {
		err = allProvidersScopedAdminClient.Get(ctx, types.NamespacedName{
			Name: providerWsName,
		}, &kcptenancyv1alpha1.Workspace{})
		return err == nil
	}, 240*time.Second, 5*time.Second, "waiting for provider's workspace %s to be created, but has err=%v", providerWsPath, err)

	s.logger.Info().Msgf("Waiting until Provider in %s reaches Phase=Ready", providerWsPath)
	var provider pmprovidersv1alpha1.Provider
	s.Require().Eventually(func() bool {
		err = systemProviderWsClient.Get(ctx, types.NamespacedName{
			Name: "my-managed-provider",
		}, &provider)
		if err != nil {
			return false
		}
		return provider.Status.Phase == "Ready"
	}, 240*time.Second, 5*time.Second, "waiting for Provider in :root:providers:system to be Ready, but has err=%v Provider=%#v", err, provider)
	s.Require().NotNil(provider.Status.ProviderKubeconfigSecretRef, "Provider should have its ProviderKubeconfigSecretRef populated")
	s.Require().Equal(&corev1.SecretReference{
		Name:      "my-managed-provider-provider-kubeconfig",
		Namespace: "platform-mesh-system",
	}, provider.Status.ProviderKubeconfigSecretRef, "Provider on kcp side has unexpected ProviderKubeconfigSecretRef contents")

	// Validate provider kubeconfigs on kcp side and try to list ConfigMaps using that.
	createProviderClientFromSecretRefAndListConfigMaps(systemProviderWsClient, types.NamespacedName{
		Namespace: provider.Status.ProviderKubeconfigSecretRef.Namespace,
		Name:      provider.Status.ProviderKubeconfigSecretRef.Name,
	}, "kcp")

	// Now validate runtime cluster side.

	s.logger.Info().Msgf("Waiting until ManagedProvider has its Status.KubeconfigSecretRef populated")
	s.Require().Eventually(func() bool {
		err := s.client.Get(ctx, managedProviderName, &managedProvider)
		if err != nil {
			return false
		}
		return managedProvider.Status.ProviderKubeconfigSecretRef != nil
	}, 240*time.Second, 5*time.Second, "waiting for kubeconfig ref in ManagedProvider, got err=%v ManagedProvider=%#v", err, managedProvider)
	s.logger.Info().Msgf("ManagedProvider has its Status.ProviderKubeconfigSecretRef populated")
	s.Require().Equal(managedProvider.Status.ProviderKubeconfigSecretRef.Name, "my-managed-provider-provider-kubeconfig", "")
	s.Require().Equal(managedProvider.Status.ProviderKubeconfigSecretRef.Namespace, managedProviderName.Namespace, "")

	// Validate provider kubeconfigs on PM side and try to list ConfigMaps using that.
	createProviderClientFromSecretRefAndListConfigMaps(s.client, types.NamespacedName{
		Namespace: managedProvider.Status.ProviderKubeconfigSecretRef.Namespace,
		Name:      managedProvider.Status.ProviderKubeconfigSecretRef.Name,
	}, "PM")

	// Check that ManagedProvider reaches Ready phase and that the Deployment exists.

	s.logger.Info().Msgf("Waiting until ManagedProvider reaches Phase=Ready")
	s.Require().Eventually(func() bool {
		err = s.client.Get(ctx, managedProviderName, &managedProvider)
		if err != nil {
			return false
		}
		return managedProvider.Status.Phase == "Ready"
	}, 240*time.Second, 5*time.Second, "waiting for ManagedProvider to reach Phase=Ready, but has err=%s Phase=%q", err, managedProvider.Status.Phase)

	s.logger.Info().Msgf("Waiting until Deployment example-httpbin-operator appears (type=oci component)")
	s.Require().Eventually(func() bool {
		err = s.client.Get(ctx, types.NamespacedName{
			Namespace: managedProviderName.Namespace,
			Name:      "example-httpbin-operator",
		}, &appsv1.Deployment{})
		return err == nil
	}, 240*time.Second, 5*time.Second, "waiting for Deployment example-httpbin-operator, but has err=%v", err)

	s.logger.Info().Msgf("Waiting until Deployment podinfo appears (type=helm component)")
	s.Require().Eventually(func() bool {
		err = s.client.Get(ctx, types.NamespacedName{
			Namespace: managedProviderName.Namespace,
			Name:      "podinfo",
		}, &appsv1.Deployment{})
		return err == nil
	}, 240*time.Second, 5*time.Second, "waiting for Deployment podinfo, but has err=%v", err)
}

func (s *KindTestSuite) runProviderOperator() {
	appConfig := config.NewOperatorConfig()
	if err := defaults.Set(&appConfig); err != nil {
		s.logger.Error().Err(err).Msg("Failed to set default Provider operator config")
		return
	}

	appConfig.Providers.ProvidersAPIExportEndpointSliceName = "providers.platform-mesh.io"
	appConfig.Providers.ProvidersAPIExportEndpointSliceWorkspace = "root:platform-mesh-system"
	appConfig.Subroutines.Provider.Kubeconfig.Enabled = true
	appConfig.Subroutines.Provider.Workspace.Enabled = true
	appConfig.KCP = defaultKcpOperatorConfig

	commonConfig := &pmconfig.CommonServiceConfig{
		IsLocal: true,
	}

	runtimeClient, err := ctrlruntimeclient.New(s.config, ctrlruntimeclient.Options{})
	s.Require().NoError(err, "failed to create kube client for runtime cluster")

	var kcpAdminCfg *rest.Config
	s.Require().Eventually(func() bool {
		kcpAdminCfg, err = subroutines.BuildKubeconfigFromConfig(runtimeClient, &appConfig.KCP, appConfig.KCP.Url)
		return err == nil
	}, 240*time.Second, 5*time.Second, "waiting for kcp REST config")

	scopedKcpAdminCfg := rest.CopyConfig(kcpAdminCfg)
	scopedKcpAdminCfg.Host += "/clusters/" + appConfig.Providers.ProvidersAPIExportEndpointSliceWorkspace

	providersVW, err := mcapiexportprovider.New(scopedKcpAdminCfg, appConfig.Providers.ProvidersAPIExportEndpointSliceName, mcapiexportprovider.Options{
		Scheme: s.scheme,
	})
	s.Require().NoError(err, "failed to create APIExport mc provider")

	multiProvider := s.mgr.GetProvider().(*mcmultiprovider.Provider)

	err = multiProvider.AddProvider("apiexport-providers-platform-mesh", providersVW)
	s.Require().NoError(err, "failed to add apiexport-providers-platform-mesh provider")

	rec, err := providerscontroller.NewProviderReconciler(s.mgr, &appConfig, commonConfig, runtimeClient)
	s.Require().NoError(err, "failed to create ProviderReconciler controller")
	s.Require().NoError(rec.SetupWithManager(s.mgr, commonConfig), "failed to setup ProviderReconciler with manager")

	s.logger.Info().Msg("ProviderReconciler started")
}
