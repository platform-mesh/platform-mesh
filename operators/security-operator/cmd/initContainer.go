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

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/coreos/go-oidc"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/oauth2"

	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/pkg/clientreg/keycloak"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var initContainerCfg config.InitContainerConfig

var initContainerCmd = &cobra.Command{
	Use:   "init-container",
	Short: "Bootstrap Keycloak service account clients in the master realm",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		initContainerConfig, err := loadInitContainerConfig(&initContainerCfg)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to load config file, using flags/env only")
		}

		if initContainerConfig.KeycloakBaseURL == "" {
			return fmt.Errorf("keycloak-base-url is required")
		}
		if len(initContainerConfig.Clients) == 0 {
			return fmt.Errorf("at least one client must be configured")
		}

		password, err := readPasswordFromFile(initContainerConfig.PasswordFile)
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}

		issuer := fmt.Sprintf("%s/realms/master", initContainerConfig.KeycloakBaseURL)
		provider, err := oidc.NewProvider(ctx, issuer)
		if err != nil {
			return fmt.Errorf("failed to initialize OIDC provider: %w", err)
		}

		oauthCfg := oauth2.Config{
			ClientID: initContainerConfig.KeycloakClientID,
			Endpoint: provider.Endpoint(),
		}

		token, err := oauthCfg.PasswordCredentialsToken(ctx, initContainerConfig.KeycloakUser, password)
		if err != nil {
			return fmt.Errorf("failed to get token: %w", err)
		}

		httpClient := oauthCfg.Client(ctx, token)
		adminClient := keycloak.NewAdminClient(httpClient, initContainerConfig.KeycloakBaseURL, "master")

		k8sCfg := ctrl.GetConfigOrDie()

		k8sClient, err := ctrlruntimeclient.New(k8sCfg, ctrlruntimeclient.Options{})
		if err != nil {
			return fmt.Errorf("failed to create Kubernetes client: %w", err)
		}

		adminRole, err := adminClient.GetRealmRole(ctx, "admin")
		if err != nil {
			return fmt.Errorf("failed to get admin role: %w", err)
		}
		if adminRole == nil {
			return fmt.Errorf("admin role not found in master realm")
		}

		existingClients, err := adminClient.ListClients(ctx)
		if err != nil {
			return fmt.Errorf("failed to list existing clients: %w", err)
		}
		existingClientMap := make(map[string]*keycloak.ClientInfo)
		for i := range existingClients {
			existingClientMap[existingClients[i].ClientID] = &existingClients[i]
		}

		for _, clientCfg := range initContainerConfig.Clients {
			if clientCfg.SecretRef.Name == "" || clientCfg.SecretRef.Namespace == "" {
				return fmt.Errorf("client %q: secretRef name and namespace are required", clientCfg.Name)
			}

			var clientUUID string
			if existing := existingClientMap[clientCfg.Name]; existing != nil {
				log.Info().Str("clientID", clientCfg.Name).Msg("Client already exists")
				clientUUID = existing.ID
			} else {
				log.Info().Str("clientID", clientCfg.Name).Msg("Creating service account client")
				created, err := adminClient.CreateServiceAccountClient(ctx, keycloak.ServiceAccountClientConfig{
					ClientID:               clientCfg.Name,
					Name:                   clientCfg.Name,
					Enabled:                true,
					ServiceAccountsEnabled: true,
					PublicClient:           false,
				})
				if err != nil {
					return fmt.Errorf("failed to create client %q: %w", clientCfg.Name, err)
				}
				clientUUID = created.ID
			}

			clientSecret, err := adminClient.GetClientSecret(ctx, clientUUID)
			if err != nil {
				return fmt.Errorf("failed to get client secret for %q: %w", clientCfg.Name, err)
			}

			serviceAccountUser, err := adminClient.GetServiceAccountUser(ctx, clientUUID)
			if err != nil {
				return fmt.Errorf("failed to get service account user for %q: %w", clientCfg.Name, err)
			}

			if err := adminClient.AssignRealmRoleToUser(ctx, serviceAccountUser.ID, *adminRole); err != nil {
				return fmt.Errorf("failed to assign admin role to %q: %w", clientCfg.Name, err)
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clientCfg.SecretRef.Name,
					Namespace: clientCfg.SecretRef.Namespace,
				},
			}
			_, err = controllerutil.CreateOrUpdate(ctx, k8sClient, secret, func() error {
				if secret.Data == nil {
					secret.Data = make(map[string][]byte)
				}
				secret.Data["client_id"] = []byte(clientCfg.Name)
				secret.Data["client_secret"] = []byte(clientSecret)
				secret.Type = corev1.SecretTypeOpaque
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to create secret for %q: %w", clientCfg.Name, err)
			}

			log.Info().
				Str("clientID", clientCfg.Name).
				Str("secret", clientCfg.SecretRef.Namespace+"/"+clientCfg.SecretRef.Name).
				Msg("Client configured")
		}

		log.Info().Msg("Init container completed successfully")
		return nil
	},
}

func loadInitContainerConfig(cfg *config.InitContainerConfig) (*config.InitContainerConfiguration, error) {
	v := viper.New()
	v.SetConfigFile(cfg.ConfigFile)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config config.InitContainerConfiguration
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

func readPasswordFromFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read password file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
