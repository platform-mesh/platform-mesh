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

package apischema_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	listenerapischema "go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/apischema"
	apischemaMocks "go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/apischema/mocks"

	"k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/spec3"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

func TestResolveSchema(t *testing.T) {
	// prepare a valid schemaResponse JSON
	validSchemas := map[string]*spec.Schema{"a.v1.K": {}}
	resp := spec3.OpenAPI{Components: &spec3.Components{Schemas: validSchemas}}
	validJSON, err := json.Marshal(&resp)
	assert.NoError(t, err, "failed to marshal valid response")

	tests := []struct {
		name       string
		openAPIErr error
		wantErr    bool
		setSchema  func(mock openapi.GroupVersion)
	}{
		{
			name:       "successful_resolution",
			openAPIErr: nil,
			wantErr:    false,
			setSchema: func(gv openapi.GroupVersion) {
				gv.(*apischemaMocks.MockGroupVersion).
					EXPECT().
					Schema(mock.Anything).
					Return(validJSON, nil)
			},
		},
		{
			name:       "openapi_path_error",
			openAPIErr: listenerapischema.ErrGetOpenAPIPaths,
			wantErr:    true,
			setSchema:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			openAPIClient := apischemaMocks.NewMockClient(t)

			if tc.openAPIErr != nil {
				openAPIClient.EXPECT().Paths().Return(nil, tc.openAPIErr)
			} else {
				mockGV := apischemaMocks.NewMockGroupVersion(t)
				if tc.setSchema != nil {
					tc.setSchema(mockGV)
				}
				openAPIPaths := map[string]openapi.GroupVersion{
					"/v1": mockGV,
				}
				openAPIClient.EXPECT().Paths().Return(openAPIPaths, nil)
			}

			got, err := listenerapischema.NewResolver().Resolve(t.Context(), openAPIClient)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err, "unexpected error")
			assert.NotEmpty(t, got, "expected non-empty schema")
		})
	}
}
