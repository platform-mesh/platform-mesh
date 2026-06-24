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

package context

import (
	"context"
	"regexp"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

// clusterKey is the context key for storing cluster information
const clusterKey contextKey = "cluster-key"

// tokenKey is the context key for storing authentication token
const tokenKey contextKey = "token-key"

// clusterTargetKey is the context key for storing the logical cluster target
// extracted from GraphQL extensions.
const clusterTargetKey contextKey = "cluster-target-key"

// parsedRequestsKey is the context key for the pre-parsed GraphQL request body.
// Set once by the request parser middleware; consumed by downstream middlewares.
const parsedRequestsKey contextKey = "parsed-requests-key"

// SetCluster sets cluster to the request context
func SetCluster(ctx context.Context, cluster string) context.Context {
	return context.WithValue(ctx, clusterKey, cluster)
}

// SetToken sets authentication token to the request context
func SetToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey, token)
}

// GetClusterFromCtx retrieves cluster from the request context.
// Returns the cluster name and true if found, or empty string and false otherwise.
func GetClusterFromCtx(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(clusterKey).(string)
	return v, ok
}

// GetTokenFromCtx retrieves token from the request context.
// Returns the token and true if found, or empty string and false otherwise.
func GetTokenFromCtx(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(tokenKey).(string)
	return v, ok
}

// SetClusterTarget sets the logical cluster target in the request context.
func SetClusterTarget(ctx context.Context, target string) context.Context {
	return context.WithValue(ctx, clusterTargetKey, target)
}

// GetClusterTargetFromCtx retrieves the logical cluster target from the request context.
// Returns the target and true if found, or empty string and false otherwise.
func GetClusterTargetFromCtx(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(clusterTargetKey).(string)
	return v, ok
}

// GraphQLRequest holds a single parsed GraphQL request from the body.
// Shared across middlewares so the body is only parsed once.
type GraphQLRequest struct {
	Query      string         `json:"query"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// SetParsedRequests stores the pre-parsed GraphQL requests in the context.
func SetParsedRequests(ctx context.Context, reqs []GraphQLRequest) context.Context {
	return context.WithValue(ctx, parsedRequestsKey, reqs)
}

// GetParsedRequestsFromCtx retrieves the pre-parsed GraphQL requests from the context.
func GetParsedRequestsFromCtx(ctx context.Context) ([]GraphQLRequest, bool) {
	v, ok := ctx.Value(parsedRequestsKey).([]GraphQLRequest)
	return v, ok
}

var validClusterTarget = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9:_-]*$`)

const maxClusterTargetLen = 2048

func FindClusterTarget(reqs []GraphQLRequest) string {
	for _, req := range reqs {
		if req.Extensions == nil {
			continue
		}
		if v, ok := req.Extensions["clusterTarget"].(string); ok && len(v) <= maxClusterTargetLen && validClusterTarget.MatchString(v) {
			return v
		}
	}
	return ""
}
