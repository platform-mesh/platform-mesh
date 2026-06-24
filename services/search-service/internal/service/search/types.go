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
	ID               string                 `json:"id"`
	Score            float64                `json:"score"`
	Resource         string                 `json:"resource,omitempty"`
	Kind             string                 `json:"kind,omitempty"`
	Name             string                 `json:"name,omitempty"`
	Namespace        string                 `json:"namespace,omitempty"`
	APIGroup         string                 `json:"apiGroup,omitempty"`
	APIVersion       string                 `json:"apiVersion,omitempty"`
	WorkspacePath    string                 `json:"workspacePath,omitempty"`
	ClusterName      string                 `json:"clusterName,omitempty"`
	OrganizationID   string                 `json:"organizationId,omitempty"`
	OrganizationName string                 `json:"organizationName,omitempty"`
	AccountID        string                 `json:"accountId,omitempty"`
	AccountName      string                 `json:"accountName,omitempty"`
	Source           map[string]interface{} `json:"source"`
}

type SearchResourcesRequest struct {
	Organization string
}

type SearchResource struct {
	Resource         string   `json:"resource"`
	DefaultFields    []string `json:"defaultFields,omitempty"`
	FilterableFields []string `json:"filterableFields,omitempty"`
	SemanticFields   []string `json:"semanticFields,omitempty"`
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
	Sort   []interface{}
	Source map[string]interface{}
}

type OpenSearchPage struct {
	Hits              []OpenSearchHit
	AggregationValues []string
}

type OpenSearchQuery struct {
	Indices          []string
	Query            string
	Fields           []string
	Filters          map[string][]string
	Size             int
	SearchAfter      []interface{}
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
