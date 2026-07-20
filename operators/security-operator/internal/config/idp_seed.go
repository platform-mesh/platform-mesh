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

package config

import (
	"fmt"
	"os"
	"strings"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

// SeedUpstreamConfig holds upstream identity provider seed data loaded from a
// local profile ConfigMap. Disabled when the config file path is empty.
type SeedUpstreamConfig struct {
	SeedUpstreamIdentityProviders SeedUpstreamIdentityProviders `json:"seedUpstreamIdentityProviders"`
}

type SeedUpstreamIdentityProviders struct {
	// Realms lists org/realm names that receive seeded upstream providers.
	// When omitted or empty, no realm is seeded.
	Realms    []string                       `json:"realms"`
	Providers []SeedUpstreamIdentityProvider `json:"providers"`
}

// SeedUpstreamIdentityProvider reuses UpstreamIdentityProvider fields and adds
// a plaintext client secret for local seeding only.
type SeedUpstreamIdentityProvider struct {
	pmcorev1alpha1.UpstreamIdentityProvider `json:",inline"`
	ClientSecret                            string `json:"clientSecret"`
}

func LoadSeedUpstreamConfig(path string) (*SeedUpstreamConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading seed config file %q: %w", path, err)
	}

	var cfg SeedUpstreamConfig
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing seed config file %q: %w", path, err)
	}

	return &cfg, nil
}

func (c *SeedUpstreamConfig) AllowsSeedingForRealm(realm string) bool {
	if c == nil {
		return false
	}
	realms := c.SeedUpstreamIdentityProviders.Realms
	if len(realms) == 0 {
		return false
	}
	for _, allowed := range realms {
		if allowed == realm {
			return true
		}
	}
	return false
}

func UpstreamIdentityProviderClientSecretName(realm, alias string) string {
	return fmt.Sprintf("upstream-idp-client-secret-%s-%s", realm, alias)
}

func (p SeedUpstreamIdentityProvider) ToUpstreamIdentityProvider(realm string) pmcorev1alpha1.UpstreamIdentityProvider {
	// Deep copy so per-realm mutations below never touch the shared seed config,
	// which is read concurrently across reconciles (MaxConcurrentReconciles > 1).
	upstream := *p.DeepCopy()
	if upstream.OIDC == nil {
		upstream.OIDC = &pmcorev1alpha1.OIDCUpstreamConfig{}
	}
	upstream.OIDC.ClientSecretRef = corev1.SecretReference{
		Name:      UpstreamIdentityProviderClientSecretName(realm, strings.TrimSpace(upstream.Alias)),
		Namespace: "default",
	}
	return upstream
}
