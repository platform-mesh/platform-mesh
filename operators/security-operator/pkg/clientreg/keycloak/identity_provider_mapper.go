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

package keycloak

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// IdentityProviderMapperRepresentation mirrors Keycloak's identity-provider mapper API object.
type IdentityProviderMapperRepresentation struct {
	ID                    string            `json:"id,omitempty"`
	Name                  string            `json:"name,omitempty"`
	IdentityProviderAlias string            `json:"identityProviderAlias,omitempty"`
	IdentityProviderMapper string           `json:"identityProviderMapper,omitempty"`
	Config                map[string]string `json:"config,omitempty"`
}

func (c *AdminClient) ListIdentityProviderMappers(ctx context.Context, alias string) ([]IdentityProviderMapperRepresentation, error) {
	url := fmt.Sprintf(
		"%s/admin/realms/%s/identity-provider/instances/%s/mappers",
		c.baseURL,
		c.realm,
		alias,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating list identity provider mappers request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing identity provider mappers: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp, "list identity provider mappers")
	}

	var mappers []IdentityProviderMapperRepresentation
	if err := json.NewDecoder(resp.Body).Decode(&mappers); err != nil {
		return nil, fmt.Errorf("parsing identity provider mappers response: %w", err)
	}

	return mappers, nil
}

func (c *AdminClient) DeleteIdentityProviderMapper(ctx context.Context, alias, mapperID string) error {
	url := fmt.Sprintf(
		"%s/admin/realms/%s/identity-provider/instances/%s/mappers/%s",
		c.baseURL,
		c.realm,
		alias,
		mapperID,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("creating delete identity provider mapper request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deleting identity provider mapper: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return readErrorResponse(resp, "delete identity provider mapper")
	}

	return nil
}

// SyncIdentityProviderMappers deletes all identity-provider mappers on alias.
// Operator-managed brokers use an empty desired mapper set.
func (c *AdminClient) SyncIdentityProviderMappers(ctx context.Context, alias string) error {
	mappers, err := c.ListIdentityProviderMappers(ctx, alias)
	if err != nil {
		return err
	}

	for _, mapper := range mappers {
		if mapper.ID == "" {
			continue
		}
		if err := c.DeleteIdentityProviderMapper(ctx, alias, mapper.ID); err != nil {
			return err
		}
	}

	return nil
}
