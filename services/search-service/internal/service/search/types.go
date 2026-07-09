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

package search

import "context"

type SearchRequest struct {
	Organization string
	User         string
	Query        string
	Mode         string
	Resource     string
	Filters      map[string][]string
	Limit        int
	Cursor       string
}

type SearchResponse struct {
	Results    []SearchHit `json:"results"`
	NextCursor *string     `json:"nextCursor"`
}

type SearchHit struct {
	ID               string         `json:"id"`
	Score            float64        `json:"score"`
	Resource         string         `json:"resource,omitzero"`
	Kind             string         `json:"kind,omitzero"`
	Name             string         `json:"name,omitzero"`
	Namespace        string         `json:"namespace,omitzero"`
	APIGroup         string         `json:"apiGroup,omitzero"`
	APIVersion       string         `json:"apiVersion,omitzero"`
	WorkspacePath    string         `json:"workspacePath,omitzero"`
	ClusterName      string         `json:"clusterName,omitzero"`
	OrganizationID   string         `json:"organizationId,omitzero"`
	OrganizationName string         `json:"organizationName,omitzero"`
	AccountID        string         `json:"accountId,omitzero"`
	AccountName      string         `json:"accountName,omitzero"`
	Source           map[string]any `json:"source"`
}

type SearchResourcesRequest struct {
	Organization string
}

type SearchResource struct {
	Resource         string   `json:"resource"`
	DefaultFields    []string `json:"defaultFields,omitzero"`
	FilterableFields []string `json:"filterableFields,omitzero"`
	SemanticFields   []string `json:"semanticFields,omitzero"`
}

type SearchResourcesResponse struct {
	Resources []SearchResource `json:"resources"`
}

type FilterValuesRequest struct {
	Organization string
	User         string
	Resource     string
	Field        string
	Query        string
	Filters      map[string][]string
	Limit        int
}

type FilterValuesResponse struct {
	Values []string `json:"values"`
}

type SearchIndexRef struct {
	Resource              string
	IndexName             string
	IndexPrefix           string
	OrganizationClusterID string
	DefaultFields         []string
	FilterableFields      []string
	SemanticFields        []string
	Group                 string
	Version               string
}

type OpenSearchHit struct {
	Index  string
	ID     string
	Score  float64
	Sort   []any
	Source map[string]any
}

type OpenSearchPage struct {
	Hits              []OpenSearchHit
	AggregationValues []string
}

type OpenSearchQuery struct {
	Indices          []string
	Query            string
	Mode             string
	Fields           []string
	SemanticFields   []string
	Filters          map[string][]string
	Size             int
	SearchAfter      []any
	AggregationField string
}

type AuthorizationRequest struct {
	Organization string
	User         string
	Relation     string
	Hits         []OpenSearchHit
}

type AuthorizationResult struct {
	Allowed               []bool
	DroppedMissingContext int
	Denied                int
	Calls                 int
}

type SearchIndexResolver interface {
	ResolveIndex(ctx context.Context, org, resource string) (SearchIndexRef, error)
	ListIndices(ctx context.Context, org string) ([]SearchIndexRef, error)
}

type OpenSearchSearcher interface {
	Search(ctx context.Context, req OpenSearchQuery) (OpenSearchPage, error)
}

type FGAAuthorizer interface {
	FilterAuthorized(ctx context.Context, req AuthorizationRequest) (AuthorizationResult, error)
}

type OrgAccessValidator interface {
	ValidateTokenForOrg(ctx context.Context, authHeader, org string) (bool, error)
}
