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

package policy_services

import (
	"context"
	"fmt"
	"strings"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/machinebox/graphql"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/logger"
)

type GraphqlData struct {
	TenantInfo TenantResponse
}

type TenantResponse struct {
	TenantId string
}

type TenantKey string

type TenantRetrieverService struct {
	tenantReader TenantIdReader
	tenantCache  *lru.Cache[TenantKey, string]
}

type GraphqlClient interface {
	Run(ctx context.Context, req *graphql.Request, resp any) error
}

type TenantIdReader interface {
	Read(parentCtx context.Context) (string, error)
}

// NewCustomTenantRetriever Creates a tenant retriever that has a custom implementation
// of the TenantIdReader interface. The result of the reader will be cached.
// A local and a graphql tenant Reader are already contained in this package.
func NewCustomTenantRetriever(tr TenantIdReader) *TenantRetrieverService {
	cache, err := lru.New[TenantKey, string](5)
	if err != nil {
		logger.StdLogger.Error().Err(err).Msg("cannot initialize cache, working without memory")
	}

	return &TenantRetrieverService{
		tenantReader: tr,
		tenantCache:  cache,
	}
}

// TenantRetriever Retrieves a tenant by a issuer/audiences from the context. It uses the iam service's graphql endpoint.
// The responses are cached in memory because tenant mappings do not change frequently
// It uses the tenantInfo query internally.
type TenantRetriever interface {
	RetrieveTenant(ctx context.Context) (string, error)
}

func (tenantRetriever *TenantRetrieverService) RetrieveTenant(ctx context.Context) (string, error) {
	webToken, err := pmcontext.GetWebTokenFromContext(ctx)
	if err != nil {
		return "", nil
	}
	if webToken.Issuer == "" {
		return "", nil
	}
	if len(webToken.Audiences) == 0 {
		return "", nil
	}

	key := TenantKey(fmt.Sprintf("%s-%s", webToken.Issuer, strings.Join(webToken.Audiences, "-")))
	return tenantRetriever.RetrieveOrAdd(ctx, key)
}

func (tenantRetriever *TenantRetrieverService) RetrieveOrAdd(ctx context.Context, key TenantKey) (string, error) {
	cachedTenant, found := tenantRetriever.tenantCache.Get(key)
	if found {
		return cachedTenant, nil
	}

	tenantId, err := tenantRetriever.tenantReader.Read(ctx)
	if err != nil {
		return tenantId, err
	}

	tenantRetriever.tenantCache.Add(key, tenantId)
	return tenantId, nil
}
