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

package directive

import (
	"context"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/logger"
)

func TestSetTenantToContextForTechnicalUsers(t *testing.T) {
	tests := []struct {
		name           string
		ctx            context.Context
		args           map[string]interface{}
		expectedTenant string
		expectError    bool
	}{
		{
			name: "Technical user with tenantId",
			ctx:  pmcontext.AddIsTechnicalIssuerToContext(context.Background()),
			args: map[string]interface{}{
				"tenantId": "tenant123",
			},
			expectedTenant: "tenant123",
			expectError:    false,
		},
		{
			name: "Technical user with nil tenantId",
			ctx:  pmcontext.AddIsTechnicalIssuerToContext(context.Background()),
			args: map[string]interface{}{
				"tenantId": (*string)(nil),
			},
			expectedTenant: "",
			expectError:    true,
		},
		{
			name: "Non-technical user without spiffee",
			ctx:  context.Background(),
			args: map[string]interface{}{
				"tenantId": "tenant123",
			},
			expectedTenant: "",
			expectError:    false,
		},
		{
			name: "Non-technical user with spiffee",
			ctx:  pmcontext.AddSpiffeToContext(context.Background(), "spiffee123"),
			args: map[string]interface{}{
				"tenantId": "tenant123",
			},
			expectedTenant: "tenant123",
			expectError:    false,
		},
		{
			name: "Non-technical user with spiffee (*string)",
			ctx:  pmcontext.AddIsTechnicalIssuerToContext(context.Background()),
			args: map[string]interface{}{
				"tenantId": ptr.To("tenant123"),
			},
			expectedTenant: "tenant123",
			expectError:    false,
		},
		{
			name: "Non-technical user with spiffee (*string empty)",
			ctx:  pmcontext.AddIsTechnicalIssuerToContext(context.Background()),
			args: map[string]interface{}{
				"tenantId": ptr.To(""),
			},
			expectedTenant: "",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the field context
			fc := &graphql.FieldContext{
				Args: tt.args,
			}
			ctx := graphql.WithFieldContext(tt.ctx, fc)

			// Create a logger
			l, _ := logger.New(logger.DefaultConfig())

			// Call the function
			newCtx, err := setTenantToContextForTechnicalUsers(ctx, l)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.expectedTenant != "" {
					tenantID, err := pmcontext.GetTenantFromContext(newCtx)
					assert.Nil(t, err)
					assert.Equal(t, tt.expectedTenant, tenantID)
				}
			}
		})
	}
}
