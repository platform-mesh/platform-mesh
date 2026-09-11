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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.platform-mesh.io/security-operator/internal/util"
)

// OrganizationDomainRepresentation mirrors Keycloak organization domain fields.
type OrganizationDomainRepresentation struct {
	Name     string `json:"name"`
	Verified bool   `json:"verified"`
}

// OrganizationRepresentation mirrors Keycloak's organization representation for
// the Admin REST API.
type OrganizationRepresentation struct {
	ID      string                             `json:"id,omitempty"`
	Name    string                             `json:"name,omitempty"`
	Alias   string                             `json:"alias,omitempty"`
	Enabled bool                               `json:"enabled"`
	Domains []OrganizationDomainRepresentation `json:"domains,omitempty"`
}

func (c *AdminClient) ListOrganizations(ctx context.Context) ([]OrganizationRepresentation, error) {
	// Keycloak paginates the organizations endpoint, so page through all results;
	// callers rely on seeing every organization to match one by domain.
	const pageSize = 100

	var organizations []OrganizationRepresentation
	for first := 0; ; first += pageSize {
		page, err := c.listOrganizationsPage(ctx, first, pageSize)
		if err != nil {
			return nil, err
		}
		organizations = append(organizations, page...)
		if len(page) < pageSize {
			break
		}
	}

	return organizations, nil
}

func (c *AdminClient) listOrganizationsPage(ctx context.Context, first, maxResults int) ([]OrganizationRepresentation, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/organizations?first=%d&max=%d", c.baseURL, c.realm, first, maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list organizations request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list organizations: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp, "list organizations")
	}

	var organizations []OrganizationRepresentation
	if err := json.NewDecoder(resp.Body).Decode(&organizations); err != nil {
		return nil, fmt.Errorf("failed to parse organizations response: %w", err)
	}

	return organizations, nil
}

func (c *AdminClient) GetOrganizationByDomain(
	ctx context.Context,
	domain string,
) (*OrganizationRepresentation, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, nil
	}

	organizations, err := c.ListOrganizations(ctx)
	if err != nil {
		return nil, err
	}

	for i := range organizations {
		org := &organizations[i]
		for _, d := range org.Domains {
			if strings.EqualFold(d.Name, domain) {
				return org, nil
			}
		}
	}

	return nil, nil
}

func (c *AdminClient) CreateOrganization(
	ctx context.Context,
	rep OrganizationRepresentation,
) error {
	body, err := json.Marshal(rep)
	if err != nil {
		return fmt.Errorf("failed to marshal organization: %w", err)
	}

	url := fmt.Sprintf("%s/admin/realms/%s/organizations", c.baseURL, c.realm)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create organization request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create organization: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return readErrorResponse(resp, "create organization")
	}

	return nil
}

func (c *AdminClient) UpdateOrganization(
	ctx context.Context,
	orgID string,
	rep OrganizationRepresentation,
) error {
	body, err := json.Marshal(rep)
	if err != nil {
		return fmt.Errorf("failed to marshal organization: %w", err)
	}

	url := fmt.Sprintf("%s/admin/realms/%s/organizations/%s", c.baseURL, c.realm, orgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create organization update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update organization: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return readErrorResponse(resp, "update organization")
	}

	return nil
}

func (c *AdminClient) DeleteOrganization(ctx context.Context, orgID string) error {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil
	}

	url := fmt.Sprintf("%s/admin/realms/%s/organizations/%s", c.baseURL, c.realm, orgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create organization delete request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete organization: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return readErrorResponse(resp, "delete organization")
	}

	return nil
}

// CreateOrUpdateOrganizationForDomains returns an organization that owns all requested
// domains, creating or updating it when necessary. The second return value is
// true when an existing organization was matched by domain and updated in place.
//
// Warning: if a domain already belongs to a Keycloak organization created
// manually or by another process, that organization is reused and its name,
// alias, and domain list are overwritten to match the desired state.
func (c *AdminClient) CreateOrUpdateOrganizationForDomains(
	ctx context.Context,
	name, alias string,
	domains []string,
) (*OrganizationRepresentation, bool, error) {
	normalized := util.NormalizeEmailDomains(domains)
	if len(normalized) == 0 {
		return nil, false, fmt.Errorf("at least one email domain is required")
	}

	var existing *OrganizationRepresentation
	for _, domain := range normalized {
		org, err := c.GetOrganizationByDomain(ctx, domain)
		if err != nil {
			return nil, false, err
		}
		if org != nil {
			if existing != nil && existing.ID != org.ID {
				return nil, false, fmt.Errorf(
					"email domain %q is already managed by organization %q",
					domain,
					org.Name,
				)
			}
			existing = org
		}
	}

	domainReps := make([]OrganizationDomainRepresentation, len(normalized))
	for i, domain := range normalized {
		domainReps[i] = OrganizationDomainRepresentation{
			Name:     domain,
			Verified: true,
		}
	}

	desired := OrganizationRepresentation{
		Name:    name,
		Alias:   alias,
		Enabled: true,
		Domains: domainReps,
	}

	if existing == nil {
		if err := c.CreateOrganization(ctx, desired); err != nil {
			return nil, false, err
		}
		org, err := c.findOrganizationByDomains(ctx, normalized)
		return org, false, err
	}

	desired.ID = existing.ID
	if err := c.UpdateOrganization(ctx, existing.ID, desired); err != nil {
		return nil, false, err
	}

	org, err := c.findOrganizationByDomains(ctx, normalized)
	return org, true, err
}

func (c *AdminClient) findOrganizationByDomains(
	ctx context.Context,
	domains []string,
) (*OrganizationRepresentation, error) {
	for _, domain := range domains {
		org, err := c.GetOrganizationByDomain(ctx, domain)
		if err != nil {
			return nil, err
		}
		if org != nil {
			return org, nil
		}
	}
	return nil, fmt.Errorf("organization for domains %v was not found after reconcile", domains)
}
