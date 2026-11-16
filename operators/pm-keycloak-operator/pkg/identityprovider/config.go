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

package identityprovider

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type LoadedConfiguration struct {
	Backend            *NativeBackend
	Revision, Identity string
	OutputNamespace    string
	Close              func()
}

func LoadConfiguration(ctx context.Context, classes ctrlruntimeclient.Reader, private ctrlruntimeclient.Reader, class *pmidentityv1alpha1.IdentityProviderClass, namespace string, timeout time.Duration) (*LoadedConfiguration, error) {
	if class == nil || classes == nil || private == nil || namespace == "" || timeout <= 0 {
		return nil, errors.New("provider class, trusted configuration readers, runtime namespace and positive timeout are required")
	}
	ref := class.Spec.ConfigurationRef
	if ref.APIGroup != pmkeycloakv1alpha1.GroupName || ref.Kind != "KeycloakProviderConfiguration" || ref.Name == "" {
		return nil, errors.New("keycloak provider class must reference a KeycloakProviderConfiguration in the installation workspace")
	}
	config := &pmkeycloakv1alpha1.KeycloakProviderConfiguration{}
	if err := classes.Get(ctx, ctrlruntimeclient.ObjectKey{Name: ref.Name}, config); err != nil {
		return nil, fmt.Errorf("read administrator provider configuration: %w", err)
	}
	spec := config.Spec
	if spec.OutputNamespace == "" {
		spec.OutputNamespace = "default"
	}
	if problems := validation.IsDNS1123Label(spec.OutputNamespace); len(problems) != 0 {
		return nil, errors.New("provider credential output namespace is invalid")
	}
	if spec.AccessTokenLifespan == 0 {
		spec.AccessTokenLifespan = 28800
	}
	if spec.AccessTokenLifespan < 60 {
		return nil, errors.New("provider access token lifespan must be at least 60 seconds")
	}
	credentials, err := runtimeSecret(ctx, private, namespace, spec.CredentialsSecretRef.Name)
	if err != nil {
		return nil, err
	}
	if len(credentials.Data["client_id"]) == 0 || len(credentials.Data["client_secret"]) == 0 {
		return nil, errors.New("provider credentials Secret requires client_id and client_secret")
	}
	versions := []string{string(config.UID), config.ResourceVersion, string(credentials.UID), credentials.ResourceVersion}
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout, IdleConnTimeout: 90 * time.Second,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	if spec.CASecretRef != nil {
		secret, err := runtimeSecret(ctx, private, namespace, spec.CASecretRef.Name)
		if err != nil {
			return nil, err
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(secret.Data["ca.crt"]) {
			return nil, errors.New("provider CA Secret has no certificates in ca.crt")
		}
		transport.TLSClientConfig.RootCAs = roots
		versions = append(versions, string(secret.UID), secret.ResourceVersion)
	}
	smtp := maps.Clone(spec.SMTP)
	if _, containsPassword := smtp["password"]; containsPassword {
		return nil, errors.New("SMTP password must be supplied through the runtime Secret, not public provider configuration")
	}
	if spec.SMTPSecretRef != nil {
		secret, err := runtimeSecret(ctx, private, namespace, spec.SMTPSecretRef.Name)
		if err != nil {
			return nil, err
		}
		if len(secret.Data["password"]) == 0 {
			return nil, errors.New("SMTP Secret requires password")
		}
		if smtp == nil {
			smtp = map[string]string{}
		}
		smtp["password"] = string(secret.Data["password"])
		versions = append(versions, string(secret.UID), secret.ResourceVersion)
	}
	plain := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errors.New("provider redirects are forbidden")
	}}
	oauth := clientcredentials.Config{
		ClientID: string(credentials.Data["client_id"]), ClientSecret: string(credentials.Data["client_secret"]),
		TokenURL: spec.AdminURL + "/realms/master/protocol/openid-connect/token",
	}
	admin := oauth.Client(context.WithValue(ctx, oauth2.HTTPClient, plain))
	admin.Timeout, admin.CheckRedirect = timeout, plain.CheckRedirect
	backend, err := NewNativeBackend(NativeConfig{
		AdminURL: spec.AdminURL, PublicURL: spec.PublicURL, SMTP: smtp,
		RegistrationAllowed: spec.RegistrationAllowed, AccessTokenLifespan: spec.AccessTokenLifespan,
	}, admin, plain)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	data, err := json.Marshal(struct {
		Spec     pmkeycloakv1alpha1.KeycloakProviderConfigurationSpec
		Versions []string
	}{Spec: spec, Versions: versions})
	if err != nil {
		return nil, err
	}
	identityData, err := json.Marshal([]string{
		string(class.UID), string(config.UID), spec.AdminURL, spec.PublicURL, spec.OutputNamespace,
	})
	if err != nil {
		return nil, err
	}
	return &LoadedConfiguration{
		Backend: backend, Revision: fmt.Sprintf("%x", sha256.Sum256(data)),
		Identity: fmt.Sprintf("%x", sha256.Sum256(identityData)), OutputNamespace: spec.OutputNamespace,
		Close: transport.CloseIdleConnections,
	}, nil
}

func runtimeSecret(ctx context.Context, reader ctrlruntimeclient.Reader, namespace, name string) (*corev1.Secret, error) {
	if problems := validation.IsDNS1123Subdomain(name); len(problems) != 0 {
		return nil, errors.New("provider runtime Secret reference is invalid")
	}
	secret := &corev1.Secret{}
	if err := reader.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: namespace}, secret); err != nil {
		return nil, fmt.Errorf("read provider runtime Secret: %w", err)
	}
	return secret, nil
}
