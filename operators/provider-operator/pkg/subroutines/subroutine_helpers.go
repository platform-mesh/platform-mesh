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

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/provider-operator/internal/config"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

type KcpHelper interface {
	NewKcpClient(config *rest.Config, workspacePath string) (ctrlruntimeclient.Client, error)
}

type Helper struct {
}

func (h *Helper) NewKcpClient(config *rest.Config, workspacePath string) (ctrlruntimeclient.Client, error) {
	config.QPS = 1000.0
	config.Burst = 2000.0
	u, err := url.Parse(config.Host)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to parse kcp host: %s", config.Host)
	}
	config.Host = u.Scheme + "://" + u.Host + "/clusters/" + workspacePath
	scheme := runtime.NewScheme()
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(authenticationv1.AddToScheme(scheme))
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpapisv1alpha2.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(rbacv1.AddToScheme(scheme))
	utilruntime.Must(admissionregistrationv1.AddToScheme(scheme))
	utilruntime.Must(pmprovidersv1alpha1.AddToScheme(scheme))

	cl, err := ctrlruntimeclient.New(config, ctrlruntimeclient.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create KCP client: %w", err)
	}
	return cl, nil
}

func GetSecret(client ctrlruntimeclient.Client, name string, namespace string) (*corev1.Secret, error) {
	secret := corev1.Secret{}
	err := client.Get(context.Background(), types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, &secret)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get secret")
	}
	return &secret, nil
}

// BuildKubeconfigFromConfig builds a *rest.Config for the kcp admin from the cluster-admin
// certificate Secret. It is the exported equivalent of buildKubeconfigFromConfig.
func BuildKubeconfigFromConfig(client ctrlruntimeclient.Client, kcpConfig *config.KCPConfig, kcpUrl string) (*rest.Config, error) {
	secretName := kcpConfig.ClusterAdminSecretName
	secret, err := GetSecret(client, secretName, kcpConfig.Namespace)
	if err != nil {
		return nil, fmt.Errorf("getting secret %s/%s: %w", kcpConfig.Namespace, secretName, err)
	}
	if secret == nil {
		return nil, fmt.Errorf("secret %s/%s is nil", kcpConfig.Namespace, secretName)
	}
	if secret.Data == nil {
		return nil, fmt.Errorf("secret %s/%s has no Data", kcpConfig.Namespace, secretName)
	}

	// Try kubeconfig key first (Opaque secret with pre-built kubeconfig)
	if kubeconfigData, ok := secret.Data["kubeconfig"]; ok && len(kubeconfigData) > 0 {
		cfg, err := clientcmd.Load(kubeconfigData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse kubeconfig from secret %s/%s: %w", kcpConfig.Namespace, secretName, err)
		}
		// Override the server URL in all clusters with the provided kcpUrl
		for _, cluster := range cfg.Clusters {
			cluster.Server = kcpUrl
		}
		return clientcmd.NewDefaultClientConfig(*cfg, nil).ClientConfig()
	}

	// Fall back to cert-based approach (kubernetes.io/tls secret with ca.crt, tls.crt, tls.key)
	caData, ok := secret.Data["ca.crt"]
	if !ok || len(caData) == 0 {
		return nil, fmt.Errorf("secret %s/%s missing both \"kubeconfig\" and \"ca.crt\" keys", kcpConfig.Namespace, secretName)
	}
	tlsCrt, ok := secret.Data["tls.crt"]
	if !ok || len(tlsCrt) == 0 {
		return nil, fmt.Errorf("secret %s/%s missing or empty key \"tls.crt\"", kcpConfig.Namespace, secretName)
	}
	tlsKey, ok := secret.Data["tls.key"]
	if !ok || len(tlsKey) == 0 {
		return nil, fmt.Errorf("secret %s/%s missing or empty key \"tls.key\"", kcpConfig.Namespace, secretName)
	}

	cfg := clientcmdapi.NewConfig()
	cfg.Clusters = map[string]*clientcmdapi.Cluster{
		"kcp": {
			Server:                   kcpUrl,
			CertificateAuthorityData: caData,
		},
	}
	cfg.Contexts = map[string]*clientcmdapi.Context{
		"admin": {
			Cluster:  "kcp",
			AuthInfo: "admin",
		},
	}
	cfg.AuthInfos = map[string]*clientcmdapi.AuthInfo{
		"admin": {
			ClientCertificateData: tlsCrt,
			ClientKeyData:         tlsKey,
		},
	}
	cfg.CurrentContext = "admin"
	return clientcmd.NewDefaultClientConfig(*cfg, nil).ClientConfig()
}
