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
	"fmt"

	"go.platform-mesh.io/golang-commons/errors"
)

// contextKey is a private type for context keys to avoid collisions
type contextKey string

const (
	// kcpContextKey is the key for storing KCP user context
	kcpContextKey contextKey = "KCPContext"
	// clusterIdContextKey is the key for storing Cluster ID
	clusterIdContextKey contextKey = "clusterId"
)

// KCPContext holds KCP-related user information
type KCPContext struct {
	IDMTenant        string
	OrganizationName string
}

// SetKCPContext stores KCP context information in the request context
func SetKCPContext(ctx context.Context, kcpCtx KCPContext) context.Context {
	return context.WithValue(ctx, kcpContextKey, kcpCtx)
}

// GetKCPContext retrieves KCP context information from the request context
func GetKCPContext(ctx context.Context) (KCPContext, error) {
	val := ctx.Value(kcpContextKey)
	if val == nil {
		return KCPContext{}, errors.New("kcp user context not found in context")
	}

	kcpCtx, ok := val.(KCPContext)
	if !ok {
		return KCPContext{}, errors.New("invalid kcp user context type")
	}

	return kcpCtx, nil
}

// SetClusterId stores the Cluster ID in the request context
func SetClusterId(ctx context.Context, clusterId string) context.Context {
	return context.WithValue(ctx, clusterIdContextKey, clusterId)
}

// GetClusterId retrieves the Cluster ID from the request context
func GetClusterId(ctx context.Context) (string, error) {
	val := ctx.Value(clusterIdContextKey)
	if val == nil {
		return "", fmt.Errorf("account info not found in context")
	}
	clusterId, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("invalid account info type in context")
	}
	return clusterId, nil
}
