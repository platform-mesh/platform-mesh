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
	"time"

	"github.com/machinebox/graphql"
)

type graphqlTenantReader struct {
	client  GraphqlClient
	iamUrl  string
	timeout time.Duration
}

// NewTenantRetriever Creates a retriever to get tenant ids from the iam service.
// The iamUrl parameter should be the graphql endpoint of the iam-service.
func NewTenantRetriever(ctx context.Context, iamUrl string, timeout *time.Duration) *TenantRetrieverService {
	tr := &graphqlTenantReader{
		client:  createClient(ctx, iamUrl),
		iamUrl:  iamUrl,
		timeout: time.Second * 5,
	}
	if timeout != nil {
		tr.timeout = *timeout
	}

	return NewCustomTenantRetriever(tr)
}

func (r *graphqlTenantReader) Read(ctx context.Context) (string, error) {
	req := graphql.NewRequest(`
		  query {
				tenantInfo {
					tenantId
			  }
			}
	`)

	var respData GraphqlData
	if err := run(ctx, r.client, req, &respData, r.timeout); err != nil {
		return "", err
	}

	id := respData.TenantInfo.TenantId

	if id == "" {
		return "", fmt.Errorf("the tenantInfo query returned no tenant id. The iam service %s was called", r.iamUrl)
	}

	return id, nil
}
