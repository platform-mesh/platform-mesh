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

package subroutines

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"time"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/internal/metrics"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kcpapiv1alpha "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
)

// HelmGetter is an interface for getting Helm releases
type HelmGetter interface {
	GetRelease(ctx context.Context, client ctrlruntimeclient.Client, name, ns string) (*unstructured.Unstructured, error)
}

// DefaultHelmGetter is the default implementation of HelmGetter
type DefaultHelmGetter struct{}

// GetRelease implements HelmGetter interface
func (g DefaultHelmGetter) GetRelease(ctx context.Context, cli ctrlruntimeclient.Client, name, ns string) (*unstructured.Unstructured, error) {
	return getHelmRelease(ctx, cli, name, ns)
}

func NewProviderSecretSubroutine(
	client ctrlruntimeclient.Client,
	helper KcpHelper,
	helm HelmGetter,
	kcpUrl string,
) *ProvidersecretSubroutine {
	sub := &ProvidersecretSubroutine{
		client:    client,
		kcpUrl:    kcpUrl,
		kcpHelper: helper,
		helm:      helm,
	}
	return sub
}

type ProvidersecretSubroutine struct {
	client    ctrlruntimeclient.Client
	kcpHelper KcpHelper
	kcpUrl    string
	helm      HelmGetter
}

const (
	ProvidersecretSubroutineName         = "ProvidersecretSubroutine"
	ProvidersecretSubroutineFinalizer    = "platform-mesh.core.platform-mesh.io/finalizer"
	KcpOperatorAdminKubeconfigSecretName = "kubeconfig-kcp-admin"
)

func (r *ProvidersecretSubroutine) Finalize(
	ctx context.Context, runtimeObj ctrlruntimeclient.Object,
) (subroutines.Result, error) {
	return subroutines.OK(), nil // TODO: Implement
}

func (r *ProvidersecretSubroutine) Process(
	ctx context.Context, runtimeObj ctrlruntimeclient.Object,
) (res subroutines.Result, err error) {
	start := time.Now()
	defer func() {
		labelResult := "success"
		if err != nil {
			labelResult = "error"
		}
		metrics.SubroutineTotal.WithLabelValues(r.GetName(), labelResult).Inc()
		metrics.SubroutineDuration.WithLabelValues(r.GetName()).Observe(time.Since(start).Seconds())
	}()
	operatorCfg := pmconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)

	scheme := r.client.Scheme()
	if scheme == nil {
		return subroutines.StopWithRequeue(DefaultRequeueInterval, "client scheme is nil"), nil
	}

	instance := runtimeObj.(*pmcorev1alpha1.PlatformMesh)
	log := logger.LoadLoggerFromContext(ctx)

	// Wait for kcp release to be ready before continuing
	rootShard := &unstructured.Unstructured{}
	rootShard.SetGroupVersionKind(schema.GroupVersionKind{Group: "operator.kcp.io", Version: "v1alpha1", Kind: "RootShard"})
	// Wait for root shard to be ready
	err = r.client.Get(ctx, types.NamespacedName{Name: operatorCfg.KCP.RootShardName, Namespace: operatorCfg.KCP.Namespace}, rootShard)
	if err != nil || !matchesConditionWithStatus(rootShard, "Available", "True") {
		log.Info().Msg("RootShard is not ready..")
		return subroutines.StopWithRequeue(DefaultRequeueInterval, "RootShard is not ready"), nil
	}

	frontProxy := &unstructured.Unstructured{}
	frontProxy.SetGroupVersionKind(schema.GroupVersionKind{Group: "operator.kcp.io", Version: "v1alpha1", Kind: "FrontProxy"})
	// Wait for root shard to be ready
	err = r.client.Get(ctx, types.NamespacedName{Name: operatorCfg.KCP.FrontProxyName, Namespace: operatorCfg.KCP.Namespace}, frontProxy)

	if err != nil || !matchesConditionWithStatus(frontProxy, "Available", "True") {
		log.Info().Msg("FrontProxy is not ready..")
		return subroutines.StopWithRequeue(DefaultRequeueInterval, "FrontProxy is not ready"), nil
	}

	// Determine which provider connections to use based on configuration:
	var providers []pmcorev1alpha1.ProviderConnection
	hasProv := len(instance.Spec.Kcp.ProviderConnections) > 0
	hasExtraProv := len(instance.Spec.Kcp.ExtraProviderConnections) > 0

	switch {
	case !hasProv && !hasExtraProv:
		// Nothing configured -> use default providers
		providers = DefaultProviderConnections
	case !hasProv && hasExtraProv:
		// Only extra providers configured - use default + extra providers
		providers = append(DefaultProviderConnections, instance.Spec.Kcp.ExtraProviderConnections...)
	case hasProv && !hasExtraProv:
		// Only providers configured -> use only specified providers
		providers = instance.Spec.Kcp.ProviderConnections
	default:
		// Both providers and extra providers configured -> use specified + extra providers
		providers = append(instance.Spec.Kcp.ProviderConnections, instance.Spec.Kcp.ExtraProviderConnections...)
	}

	if HasFeatureToggle(instance, "feature-enable-terminal-controller-manager") == "true" {
		providers = append(providers, pmcorev1alpha1.ProviderConnection{
			Path:      "root:platform-mesh-system",
			Secret:    "terminal-controller-manager-kubeconfig",
			AdminAuth: ptr.To(true),
		})
	}

	// Build kcp kubeonfig
	cfg, err := buildKubeconfig(ctx, r.client, r.kcpUrl)
	if err != nil {
		log.Error().Err(err).Msg("Failed to build kubeconfig")
		return subroutines.OK(), gcerrors.Wrap(err, "Failed to build kubeconfig")
	}
	hasScoped := false
	for _, pc := range providers {
		if _, connErr := r.HandleProviderConnection(ctx, instance, pc, cfg); connErr != nil {
			log.Error().Err(connErr).Msg("Failed to handle provider connection")
			return subroutines.OK(), connErr
		}
		hasScoped = hasScoped || !ptr.Deref(pc.AdminAuth, false)
	}
	if hasScoped {
		// Scoped tokens expire, come back to renew them in time.
		return subroutines.OKWithRequeue(scopedTokenRenewalCheckInterval), nil
	}
	return subroutines.OK(), nil
}

func (r *ProvidersecretSubroutine) Finalizers(instance ctrlruntimeclient.Object) []string { // coverage-ignore
	return []string{ProvidersecretSubroutineFinalizer}
}

func (r *ProvidersecretSubroutine) GetName() string {
	return ProvidersecretSubroutineName
}

func (r *ProvidersecretSubroutine) HandleProviderConnection(
	ctx context.Context, instance *pmcorev1alpha1.PlatformMesh, pc pmcorev1alpha1.ProviderConnection, cfg *rest.Config,
) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)
	operatorCfg := pmconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)

	if !ptr.Deref(pc.AdminAuth, false) {
		if err := writeScopedKubeconfigToSecret(ctx, r.client, r.kcpHelper, cfg, instance, pc); err != nil {
			log.Error().Err(err).Str("secret", pc.Secret).Msg("Failed to write scoped provider kubeconfig")
			return subroutines.OK(), err
		}
		return subroutines.OK(), nil
	}

	var address *url.URL

	if ptr.Deref(pc.EndpointSliceName, "") != "" {
		kcpClient, err := r.kcpHelper.NewKcpClient(cfg, pc.Path)
		if err != nil {
			log.Error().Err(err).Msg("Failed to create KCP client")
			return subroutines.OK(), err
		}

		var slice kcpapiv1alpha.APIExportEndpointSlice
		err = kcpClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: *pc.EndpointSliceName}, &slice)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get APIExportEndpointSlice")
			return subroutines.OK(), err
		}

		if len(slice.Status.APIExportEndpoints) == 0 {
			return subroutines.StopWithRequeue(DefaultRequeueInterval, "no endpoints in slice"), nil
		}

		endpointURL := slice.Status.APIExportEndpoints[0].URL
		address, err = url.Parse(endpointURL)
		if err != nil {
			log.Error().Err(err).Msg("Failed to parse endpoint URL")
			return subroutines.OK(), err
		}
	} else {
		kcpUrl, err := url.Parse(cfg.Host)
		if err != nil {
			log.Error().Err(err).Msg("Failed to parse KCP URL")
			return subroutines.OK(), err
		}
		if ptr.Deref(pc.RawPath, "") != "" {
			kcpUrl.Path = *pc.RawPath
		} else {
			kcpUrl.Path = path.Join("clusters", pc.Path)
		}
		address = kcpUrl
	}

	namespace := "platform-mesh-system"
	if ptr.Deref(pc.Namespace, "") != "" {
		namespace = *pc.Namespace
	}

	hostPort := fmt.Sprintf("https://%s-front-proxy.%s:%s", operatorCfg.KCP.FrontProxyName, operatorCfg.KCP.Namespace, operatorCfg.KCP.FrontProxyPort)
	if pc.External {
		hostPort = fmt.Sprintf("https://kcp.api.%s:%d", instance.Spec.Exposure.BaseDomain, instance.Spec.Exposure.Port)
	}
	host, err := url.JoinPath(hostPort, address.Path)
	if err != nil {
		log.Error().Err(err).Msg("Failed to join path for provider connection")
		return subroutines.OK(), err
	}

	adminKubeconfigData, err := loadKcpOperatorAdminKubeconfig(r.client, operatorCfg.KCP.Namespace)
	if err != nil {
		log.Error().Err(err).Str("secret", pc.Secret).Msg("Failed to read kcp-operator admin kubeconfig")
		return subroutines.OK(), err
	}
	trustBundle, err := buildAdminAuthTrustBundle(ctx, r.client, adminKubeconfigData, &operatorCfg)
	if err != nil {
		log.Error().Err(err).Str("secret", pc.Secret).Msg("Failed to build admin auth trust bundle from kubeconfig-kcp-admin and root shard CA")
		return subroutines.OK(), err
	}
	if err := writeProviderSecretFromKcpOperatorAdminKubeconfig(ctx, r.client, adminKubeconfigData, host, trustBundle, pc.Secret, namespace); err != nil {
		log.Error().Err(err).Msg("Failed to create or update secret")
		return subroutines.OK(), err
	}

	log.Debug().Str("secret", pc.Secret).Msg("Created or updated provider secret")

	return subroutines.OK(), nil
}

func (r *ProvidersecretSubroutine) HandleInitializerConnection(
	ctx context.Context, instance *pmcorev1alpha1.PlatformMesh, ic pmcorev1alpha1.InitializerConnection, restCfg *rest.Config,
) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)

	kcpClient, err := r.kcpHelper.NewKcpClient(restCfg, ic.Path)
	if err != nil {
		log.Error().Err(err).Msg("creating kcp client for initializer")
		return subroutines.OK(), err
	}

	wt := &kcptenancyv1alpha.WorkspaceType{}
	if err := kcpClient.Get(ctx, types.NamespacedName{Name: ic.WorkspaceTypeName}, wt); err != nil {
		log.Error().Err(err).Msg("getting WorkspaceType")
		return subroutines.OK(), err
	}
	if len(wt.Status.VirtualWorkspaces) == 0 {
		err = fmt.Errorf("no virtual workspaces found in %s", ic.WorkspaceTypeName)
		log.Error().Err(err).Msg("bad WorkspaceType")
		return subroutines.StopWithRequeue(DefaultRequeueInterval, err.Error()), nil
	}

	newConfig := rest.CopyConfig(restCfg)
	apiConfig := restConfigToAPIConfig(newConfig)
	curr := apiConfig.CurrentContext
	cluster := apiConfig.Contexts[curr].Cluster
	apiConfig.Clusters[cluster].Server = wt.Status.VirtualWorkspaces[0].URL

	var url *url.URL
	url, err = url.Parse(wt.Status.VirtualWorkspaces[0].URL)
	if err != nil {
		log.Error().Err(err).Msg("parsing virtual workspace URL")
		return subroutines.OK(), err
	}
	operatorCfg := pmconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)
	url.Host = fmt.Sprintf("%s-front-proxy:%s", operatorCfg.KCP.FrontProxyName, operatorCfg.KCP.FrontProxyPort)
	apiConfig.Clusters[cluster].Server = url.String()
	log.Debug().Str("url", url.String()).Msg("modified virtual workspace URL")

	data, err := clientcmd.Write(*apiConfig)
	if err != nil {
		log.Error().Err(err).Msg("writing modified kubeconfig")
		return subroutines.OK(), err
	}

	namespace := "platform-mesh-system"
	if ic.Namespace != "" {
		namespace = ic.Namespace
	}
	initializerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ic.Secret,
			Namespace: namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.client, initializerSecret, func() error {
		initializerSecret.Data = map[string][]byte{"kubeconfig": data}
		return err
	})
	if err != nil {
		log.Error().Err(err).Msg("creating/updating initializer Secret")
		return subroutines.OK(), err
	}

	return subroutines.OK(), nil
}

// loadKcpOperatorAdminKubeconfig reads kubeconfig-kcp-admin from the kcp workspace namespace
// (PlatformMesh/operator KCP config; same as helm infra .Values.kcp.namespace).
func loadKcpOperatorAdminKubeconfig(k8sClient ctrlruntimeclient.Client, namespace string) ([]byte, error) {
	if namespace == "" {
		return nil, fmt.Errorf("read %s: kcp namespace is empty", KcpOperatorAdminKubeconfigSecretName)
	}
	secret, err := GetSecret(k8sClient, KcpOperatorAdminKubeconfigSecretName, namespace)
	if err != nil {
		return nil, fmt.Errorf("read %s from namespace %s: %w", KcpOperatorAdminKubeconfigSecretName, namespace, err)
	}
	if secret == nil || len(secret.Data["kubeconfig"]) == 0 {
		return nil, fmt.Errorf("secret %s/%s missing key kubeconfig", namespace, KcpOperatorAdminKubeconfigSecretName)
	}
	return secret.Data["kubeconfig"], nil
}

// buildAdminAuthTrustBundle merges cluster certificate-authority-data from kubeconfig-kcp-admin with PEMs from
// Secret {RootShardName}-ca (tls.crt, and ca.crt when it differs from tls.crt).
func buildAdminAuthTrustBundle(ctx context.Context, k8sClient ctrlruntimeclient.Client, adminKubeconfigData []byte, operatorCfg *config.OperatorConfig) ([]byte, error) {
	apiCfg, err := clientcmd.Load(adminKubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig-kcp-admin for CA: %w", err)
	}
	var bundle []byte
	for _, c := range apiCfg.Clusters {
		if c == nil || len(c.CertificateAuthorityData) == 0 {
			continue
		}
		bundle = appendPEMCertsDedupe(bundle, c.CertificateAuthorityData)
	}
	if operatorCfg != nil && operatorCfg.KCP.RootShardName != "" && operatorCfg.KCP.Namespace != "" {
		secretName := operatorCfg.KCP.RootShardName + "-ca"
		var rootSecret corev1.Secret
		key := types.NamespacedName{Name: secretName, Namespace: operatorCfg.KCP.Namespace}
		if err := k8sClient.Get(ctx, key, &rootSecret); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("get %s/%s: %w", operatorCfg.KCP.Namespace, secretName, err)
			}
		} else if rootSecret.Data != nil {
			tlsData := rootSecret.Data["tls.crt"]
			caKey := rootSecret.Data["ca.crt"]
			if len(tlsData) > 0 {
				bundle = appendPEMCertsDedupe(bundle, tlsData)
			}
			if len(caKey) > 0 && (len(tlsData) == 0 || string(caKey) != string(tlsData)) {
				bundle = appendPEMCertsDedupe(bundle, caKey)
			}
		}
	}
	if len(bundle) == 0 {
		return nil, fmt.Errorf("no CA certificates after merging kubeconfig-kcp-admin clusters and %s-ca", operatorCfg.KCP.RootShardName)
	}
	return bundle, nil
}

func writeProviderSecretFromKcpOperatorAdminKubeconfig(
	ctx context.Context,
	k8sClient ctrlruntimeclient.Client,
	adminKubeconfigData []byte,
	targetServerURL string,
	frontProxyCAData []byte,
	providerSecretName, providerSecretNamespace string,
) error {
	apiCfg, err := clientcmd.Load(adminKubeconfigData)
	if err != nil {
		return fmt.Errorf("load kcp-operator admin kubeconfig: %w", err)
	}
	for _, c := range apiCfg.Clusters {
		if c == nil {
			continue
		}
		if len(frontProxyCAData) > 0 {
			c.CertificateAuthorityData = frontProxyCAData
			c.CertificateAuthority = ""
			c.InsecureSkipTLSVerify = false
		}
		c.Server = targetServerURL
	}
	out, err := clientcmd.Write(*apiCfg)
	if err != nil {
		return fmt.Errorf("serialize provider kubeconfig: %w", err)
	}
	providerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      providerSecretName,
			Namespace: providerSecretNamespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, k8sClient, providerSecret, func() error {
		providerSecret.Data = map[string][]byte{
			"kubeconfig": out,
		}
		return nil
	})
	return err
}

func restConfigToAPIConfig(restCfg *rest.Config) *clientcmdapi.Config {
	if restCfg == nil {
		return nil
	}

	clientConfig := &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"default-cluster": {
				Server:                   restCfg.Host,
				CertificateAuthorityData: restCfg.CAData,
				InsecureSkipTLSVerify:    restCfg.Insecure,
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"default-auth": {
				Token:                 restCfg.BearerToken,
				ClientCertificateData: restCfg.CertData,
				ClientKeyData:         restCfg.KeyData,
				Username:              restCfg.Username,
				Password:              restCfg.Password,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"default-context": {
				Cluster:  "default-cluster",
				AuthInfo: "default-auth",
			},
		},
		CurrentContext: "default-context",
	}

	return clientConfig
}
