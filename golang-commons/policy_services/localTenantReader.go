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
)

type localTenantReader struct {
	tenantId string
}

// NewLocalTenantRetriever Creates a Tenant reader that returns a hardcoded tenant id for local testing.
// The idea is to use a tenant id that can be set to the environment, so you do not need an iam service running locally
func NewLocalTenantRetriever(tenantId string) *TenantRetrieverService {
	tr := &localTenantReader{
		tenantId: tenantId,
	}
	return NewCustomTenantRetriever(tr)
}

func (reader localTenantReader) Read(context.Context) (string, error) {
	return reader.tenantId, nil
}
