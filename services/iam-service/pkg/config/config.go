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
	"os"
	"time"

	"github.com/spf13/pflag"
)

type IDMConfig struct {
	ExcludedTenants []string
}

type OpenFGAConfig struct {
	GRPCAddr      string
	StoreCacheTTL time.Duration
}

type JWTConfig struct {
	UserIDClaim string
}

type KeycloakCacheConfig struct {
	Enabled bool
	TTL     time.Duration
}

type KeycloakConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	PageSize     int
	Cache        KeycloakCacheConfig
}

type PaginationConfig struct {
	DefaultLimit int
	DefaultPage  int
}

type SortingConfig struct {
	DefaultField     string
	DefaultDirection string
}

type RolesConfig struct {
	FilePath string
}

type ServiceConfig struct {
	Port       int
	OpenFGA    OpenFGAConfig
	JWT        JWTConfig
	IDM        IDMConfig
	Keycloak   KeycloakConfig
	Pagination PaginationConfig
	Sorting    SortingConfig
	Roles      RolesConfig
}

func NewServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		Port: 8080,
		OpenFGA: OpenFGAConfig{
			GRPCAddr:      "openfga:8081",
			StoreCacheTTL: 5 * time.Minute,
		},
		JWT: JWTConfig{
			UserIDClaim: "sub",
		},
		IDM: IDMConfig{
			ExcludedTenants: []string{"welcome"},
		},
		Keycloak: KeycloakConfig{
			BaseURL:      "https://portal.dev.local:8443/keycloak",
			ClientID:     "iam",
			ClientSecret: os.Getenv("KEYCLOAK_CLIENT_SECRET"),
			PageSize:     100,
			Cache: KeycloakCacheConfig{
				Enabled: true,
				TTL:     time.Hour,
			},
		},
		Pagination: PaginationConfig{
			DefaultLimit: 10,
			DefaultPage:  1,
		},
		Sorting: SortingConfig{
			DefaultField:     "LastName",
			DefaultDirection: "ASC",
		},
		Roles: RolesConfig{
			FilePath: "input/roles.yaml",
		},
	}
}

func (c *ServiceConfig) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&c.Port, "port", c.Port, "Set the service port")

	fs.StringVar(&c.OpenFGA.GRPCAddr, "openfga-grpc-addr", c.OpenFGA.GRPCAddr, "Set OpenFGA gRPC address")
	fs.DurationVar(&c.OpenFGA.StoreCacheTTL, "openfga-store-cache-ttl", c.OpenFGA.StoreCacheTTL, "Set OpenFGA store cache TTL")

	fs.StringVar(&c.JWT.UserIDClaim, "jwt-user-id-claim", c.JWT.UserIDClaim, "Set JWT user id claim")
	fs.StringSliceVar(&c.IDM.ExcludedTenants, "excluded-tenants", c.IDM.ExcludedTenants, "Set IDM excluded tenants")

	fs.StringVar(&c.Keycloak.BaseURL, "keycloak-base-url", c.Keycloak.BaseURL, "Set Keycloak base URL")
	fs.StringVar(&c.Keycloak.ClientID, "keycloak-client-id", c.Keycloak.ClientID, "Set Keycloak client ID")
	fs.IntVar(&c.Keycloak.PageSize, "keycloak-page-size", c.Keycloak.PageSize, "Set Keycloak page size")
	fs.BoolVar(&c.Keycloak.Cache.Enabled, "keycloak-cache-enabled", c.Keycloak.Cache.Enabled, "Enable keycloak user cache")
	fs.DurationVar(&c.Keycloak.Cache.TTL, "keycloak-user-cache-ttl", c.Keycloak.Cache.TTL, "Set keycloak user cache TTL")

	fs.IntVar(&c.Pagination.DefaultLimit, "pagination-default-limit", c.Pagination.DefaultLimit, "Set default pagination limit")
	fs.IntVar(&c.Pagination.DefaultPage, "pagination-default-page", c.Pagination.DefaultPage, "Set default pagination page")
	fs.StringVar(&c.Sorting.DefaultField, "sorting-default-field", c.Sorting.DefaultField, "Set default sorting field")
	fs.StringVar(&c.Sorting.DefaultDirection, "sorting-default-direction", c.Sorting.DefaultDirection, "Set default sorting direction")
	fs.StringVar(&c.Roles.FilePath, "roles-file-path", c.Roles.FilePath, "Set roles file path")
}
