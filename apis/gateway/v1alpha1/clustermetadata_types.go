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

package v1alpha1

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kube-openapi/pkg/spec3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Schema represents the data extracted from a schema file
// +kubebuilder:object:generate=false
type Schema struct {
	Components      *spec3.Components `json:"components,omitempty"`
	ClusterMetadata *ClusterMetadata  `json:"x-cluster-metadata,omitempty"`
}

// ClusterMetadataFunc is a function type that returns ClusterMetadata for a given cluster name
// +kubebuilder:object:generate=false
type ClusterMetadataFunc func(clusterName string) (*ClusterMetadata, error)

// ClusterURLResolver is function that will resolve cluster url for a given cluster name
// +kubebuilder:object:generate=false
type ClusterURLResolver func(currentURL, clusterName string) (string, error)

// DefaultClusterURLResolverFunc is the default implementation that returns the URL unchanged
func DefaultClusterURLResolverFunc(url, clusterName string) (string, error) {
	return url, nil
}

// These following types are used to store cluster connection metadata in schema files
// They are not used directly in Kubernetes resources.

// ClusterMetadata represents the cluster connection metadata stored in schema files.
type ClusterMetadata struct {
	Host                string        `json:"host"`
	Path                string        `json:"path,omitempty"`
	RequestPathTemplate string        `json:"requestPathTemplate,omitempty"`
	IntrospectionPath   string        `json:"introspectionPath,omitempty"`
	Auth                *AuthMetadata `json:"auth,omitempty"`
	CA                  *CAMetadata   `json:"ca,omitempty"`
}

type AuthenticationType string

const (
	AuthTypeToken          AuthenticationType = "token"
	AuthTypeKubeconfig     AuthenticationType = "kubeconfig"
	AuthTypeClientCert     AuthenticationType = "clientCert"
	AuthTypeServiceAccount AuthenticationType = "serviceAccount"
)

// AuthMetadata represents authentication information
type AuthMetadata struct {
	Type       AuthenticationType `json:"type"`
	Token      string             `json:"token,omitempty"`
	Kubeconfig string             `json:"kubeconfig,omitempty"`
	CertData   string             `json:"certData,omitempty"`
	KeyData    string             `json:"keyData,omitempty"`
	// ServiceAccount fields for SA token generation
	SAName      string   `json:"saName,omitempty"`
	SANamespace string   `json:"saNamespace,omitempty"`
	SAAudience  []string `json:"saAudience,omitempty"`
}

// CAMetadata represents CA certificate information
type CAMetadata struct {
	Data string `json:"data"`
}

// buildConfigFromMetadata creates a rest.Config from base64-encoded metadata (used by gateway)
func BuildRestConfigFromMetadata(metadata ClusterMetadata) (*rest.Config, error) {
	return buildConfigFromMetadata(metadata)
}

// BuildClusterMetadataFromClusterAccess builds ClusterMetadata from ClusterAccess by reading secrets
func BuildClusterMetadataFromClusterAccess(ctx context.Context, ca ClusterAccess, c client.Client) (*ClusterMetadata, error) {
	return buildClusterMetadataFromClusterAccess(ctx, ca, c)
}

// buildClusterMetadataFromClusterAccess builds ClusterMetadata from ClusterAccess
func buildClusterMetadataFromClusterAccess(ctx context.Context, ca ClusterAccess, c client.Client) (*ClusterMetadata, error) {
	metadata := &ClusterMetadata{
		Host:                ca.Spec.Host,
		Path:                ca.Spec.Path,
		RequestPathTemplate: ca.Spec.RequestPathTemplate,
		IntrospectionPath:   ca.Spec.IntrospectionPath,
	}

	// Handle CA configuration
	if ca.Spec.CA != nil && ca.Spec.CA.SecretRef != nil {
		caData, err := readSecretKey(ctx, c, ca.Spec.CA.SecretRef)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA secret: %w", err)
		}
		metadata.CA = &CAMetadata{
			Data: base64.StdEncoding.EncodeToString(caData),
		}
	}

	// Handle authentication configuration
	if ca.Spec.Auth == nil {
		return metadata, nil
	}

	auth := ca.Spec.Auth
	switch {
	case auth.TokenSecretRef != nil:
		tokenData, err := readSecretKey(ctx, c, auth.TokenSecretRef)
		if err != nil {
			return nil, fmt.Errorf("failed to read token secret: %w", err)
		}
		metadata.Auth = &AuthMetadata{
			Type:  AuthTypeToken,
			Token: base64.StdEncoding.EncodeToString(tokenData),
		}

	case auth.KubeconfigSecretRef != nil:
		kubeconfigData, err := readSecretKey(ctx, c, auth.KubeconfigSecretRef)
		if err != nil {
			return nil, fmt.Errorf("failed to read kubeconfig secret: %w", err)
		}
		metadata.Auth = &AuthMetadata{
			Type:       AuthTypeKubeconfig,
			Kubeconfig: base64.StdEncoding.EncodeToString(kubeconfigData),
		}
		// If host is not explicitly set, derive it from the kubeconfig
		if metadata.Host == "" {
			clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigData)
			if err != nil {
				return nil, fmt.Errorf("failed to parse kubeconfig for host extraction: %w", err)
			}
			cfg, err := clientConfig.ClientConfig()
			if err != nil {
				return nil, fmt.Errorf("failed to get client config from kubeconfig: %w", err)
			}
			metadata.Host = cfg.Host
		}

	case auth.ClientCertificateRef != nil:
		secret := &corev1.Secret{}
		if err := c.Get(ctx, client.ObjectKey{
			Name:      auth.ClientCertificateRef.Name,
			Namespace: auth.ClientCertificateRef.Namespace,
		}, secret); err != nil {
			return nil, fmt.Errorf("failed to get client certificate secret: %w", err)
		}

		certData, ok := secret.Data[corev1.TLSCertKey]
		if !ok {
			return nil, fmt.Errorf("secret %s/%s missing key %s", auth.ClientCertificateRef.Namespace, auth.ClientCertificateRef.Name, corev1.TLSCertKey)
		}
		keyData, ok := secret.Data[corev1.TLSPrivateKeyKey]
		if !ok {
			return nil, fmt.Errorf("secret %s/%s missing key %s", auth.ClientCertificateRef.Namespace, auth.ClientCertificateRef.Name, corev1.TLSPrivateKeyKey)
		}

		metadata.Auth = &AuthMetadata{
			Type:     AuthTypeClientCert,
			CertData: base64.StdEncoding.EncodeToString(certData),
			KeyData:  base64.StdEncoding.EncodeToString(keyData),
		}

	case auth.ServiceAccountRef != nil:
		// Generate a token for the ServiceAccount using TokenRequest API
		sa := &corev1.ServiceAccount{}
		if err := c.Get(ctx, client.ObjectKey{
			Name:      auth.ServiceAccountRef.Name,
			Namespace: auth.ServiceAccountRef.Namespace,
		}, sa); err != nil {
			return nil, fmt.Errorf("failed to get service account %s/%s: %w", auth.ServiceAccountRef.Namespace, auth.ServiceAccountRef.Name, err)
		}

		tokenRequest := &authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				Audiences: auth.ServiceAccountRef.Audience,
			},
		}

		// Use configured expiration if provided
		if auth.ServiceAccountRef.TokenExpiration != nil && auth.ServiceAccountRef.TokenExpiration.Duration > 0 {
			expirationSeconds := int64(auth.ServiceAccountRef.TokenExpiration.Seconds())
			tokenRequest.Spec.ExpirationSeconds = &expirationSeconds
		}

		if err := c.SubResource("token").Create(ctx, sa, tokenRequest); err != nil {
			return nil, fmt.Errorf("failed to create token for service account %s/%s: %w", auth.ServiceAccountRef.Namespace, auth.ServiceAccountRef.Name, err)
		}

		metadata.Auth = &AuthMetadata{
			Type:        AuthTypeServiceAccount,
			Token:       base64.StdEncoding.EncodeToString([]byte(tokenRequest.Status.Token)),
			SAName:      auth.ServiceAccountRef.Name,
			SANamespace: auth.ServiceAccountRef.Namespace,
			SAAudience:  auth.ServiceAccountRef.Audience,
		}
	}

	return metadata, nil
}

// readSecretKey reads a specific key from a secret referenced by SecretKeyRef
func readSecretKey(ctx context.Context, c client.Client, ref *SecretKeyRef) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{
		Name:      ref.Name,
		Namespace: ref.Namespace,
	}, secret); err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}

	key := ref.Key
	if key == "" {
		// Use default key based on common conventions
		if _, ok := secret.Data[corev1.ServiceAccountTokenKey]; ok {
			key = corev1.ServiceAccountTokenKey
		} else {
			// Return the first key if there's only one
			if len(secret.Data) == 1 {
				for k := range secret.Data {
					key = k
					break
				}
			} else {
				return nil, fmt.Errorf("secret %s/%s has multiple keys, please specify which key to use", ref.Namespace, ref.Name)
			}
		}
	}

	data, ok := secret.Data[key]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s missing key %s", ref.Namespace, ref.Name, key)
	}

	return data, nil
}

// BuildRestConfigFromClusterAccess creates a rest.Config from ClusterAccess by reading secrets
func BuildRestConfigFromClusterAccess(ctx context.Context, ca ClusterAccess, c client.Client) (*rest.Config, error) {
	metadata, err := buildClusterMetadataFromClusterAccess(ctx, ca, c)
	if err != nil {
		return nil, err
	}
	return buildConfigFromMetadata(*metadata)
}

// buildConfigFromMetadata creates a rest.Config from base64-encoded metadata (used by gateway)
func buildConfigFromMetadata(metadata ClusterMetadata) (*rest.Config, error) {
	config := &rest.Config{
		Host: metadata.Host,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true, // Start with insecure, will be overridden if CA is provided
		},
	}

	// Handle CA data
	if metadata.CA != nil && metadata.CA.Data != "" {
		decodedCA, err := base64.StdEncoding.DecodeString(metadata.CA.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode CA data: %w", err)
		}
		config.CAData = decodedCA
		config.Insecure = false
	}

	// Handle authentication based on type if we have it
	if metadata.Auth == nil {
		return config, nil
	}

	switch metadata.Auth.Type {
	case AuthTypeToken:
		if metadata.Auth.Token != "" {
			tokenData, err := base64.StdEncoding.DecodeString(metadata.Auth.Token)
			if err != nil {
				return nil, fmt.Errorf("failed to decode token: %w", err)
			}
			config.BearerToken = string(tokenData)
		}
	case AuthTypeKubeconfig:
		if metadata.Auth.Kubeconfig != "" {
			kubeconfigData, err := base64.StdEncoding.DecodeString(metadata.Auth.Kubeconfig)
			if err != nil {
				return nil, fmt.Errorf("failed to decode kubeconfig: %w", err)
			}

			if err := configureFromKubeconfig(config, kubeconfigData); err != nil {
				return nil, fmt.Errorf("failed to configure from kubeconfig: %w", err)
			}
		}
	case AuthTypeClientCert:
		if metadata.Auth.CertData != "" && metadata.Auth.KeyData != "" {
			decodedCert, err := base64.StdEncoding.DecodeString(metadata.Auth.CertData)
			if err != nil {
				return nil, fmt.Errorf("failed to decode cert data: %w", err)
			}
			decodedKey, err := base64.StdEncoding.DecodeString(metadata.Auth.KeyData)
			if err != nil {
				return nil, fmt.Errorf("failed to decode key data: %w", err)
			}
			config.CertData = decodedCert
			config.KeyData = decodedKey
		}
	case AuthTypeServiceAccount:
		// ServiceAccount auth stores a generated token for API access
		if metadata.Auth.Token != "" {
			tokenData, err := base64.StdEncoding.DecodeString(metadata.Auth.Token)
			if err != nil {
				return nil, fmt.Errorf("failed to decode service account token: %w", err)
			}
			config.BearerToken = string(tokenData)
		}
	}

	if metadata.Host != "" {
		config.Host = metadata.Host
	}

	if config.Host == "" {
		return nil, errors.New("host must be set either in spec or derived from kubeconfig")
	}

	return config, nil
}

// configureFromKubeconfig configures authentication from kubeconfig data
func configureFromKubeconfig(config *rest.Config, kubeconfigData []byte) error {
	// Parse kubeconfig and extract auth info
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigData)
	if err != nil {
		return errors.Join(errors.New("failed to parse kubeconfig"), err)
	}

	cfg, err := clientConfig.ClientConfig()
	if err != nil {
		return errors.Join(errors.New("failed to get client config from kubeconfig"), err)
	}

	*config = *cfg
	return err
}

// BuildClusterMetadataFromConfig creates a ClusterMetadata from a rest.Config
func BuildClusterMetadataFromConfig(config *rest.Config) (*ClusterMetadata, error) {
	if config == nil {
		return nil, errors.New("config is required")
	}

	if config.Host == "" {
		return nil, errors.New("host is required in config")
	}

	metadata := &ClusterMetadata{
		Host: config.Host,
	}

	// Handle CA data
	if len(config.CAData) > 0 {
		metadata.CA = &CAMetadata{
			Data: base64.StdEncoding.EncodeToString(config.CAData),
		}
	}

	// Determine authentication type and populate auth metadata
	switch {
	case config.BearerToken != "":
		metadata.Auth = &AuthMetadata{
			Type:  AuthTypeToken,
			Token: base64.StdEncoding.EncodeToString([]byte(config.BearerToken)),
		}
	case len(config.CertData) > 0 && len(config.KeyData) > 0:
		metadata.Auth = &AuthMetadata{
			Type:     AuthTypeClientCert,
			CertData: base64.StdEncoding.EncodeToString(config.CertData),
			KeyData:  base64.StdEncoding.EncodeToString(config.KeyData),
		}
	}

	return metadata, nil
}
