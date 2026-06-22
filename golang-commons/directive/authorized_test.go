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
	"errors"
	"fmt"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/go-jose/go-jose/v4"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/vektah/gqlparser/v2/gqlerror"
	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/context/keys"
	logger "go.platform-mesh.io/golang-commons/logger/testlogger"

	"go.platform-mesh.io/golang-commons/directive/mocks"
)

const token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"

var signatureAlgorithms = []jose.SignatureAlgorithm{jose.HS256}

func String(s string) *string { return &s }

func TestAuthorized(t *testing.T) {
	directiveConfiguration.DirectivesAuthorizationEnabled = true
	testCases := []struct {
		name                string
		relation            string
		entityID            string
		entityParamName     string
		entityType          *string
		entityTypeParamName *string
		isTechnicalUser     bool
		graphqlArgs         map[string]any
		fgaMocks            func(s *mocks.OpenFGAServiceClient)
		expectedError       error
	}{
		{
			name:            "should error if the entityParamName is not part of the arguments",
			entityParamName: "non-existent",
			graphqlArgs:     map[string]any{},
			expectedError:   fmt.Errorf("unable to extract param from request for given paramName %q", "non-existent"),
		},
		{
			name:            "should error if the entityParamName is not part of the arguments for a nested value",
			entityParamName: "non-existent.nested",
			graphqlArgs: map[string]any{
				"non-existent": map[string]interface{}{},
			},
			expectedError: fmt.Errorf("unable to extract param from request for given paramName %q", "non-existent.nested"),
		},
		{
			name:            "should error if the entityParamName has the wrong type for a nested value",
			entityParamName: "non-existent.nested",
			graphqlArgs: map[string]any{
				"non-existent": "something wrong",
			},
			expectedError: fmt.Errorf("unable to extract param from request for given paramName %q, param is of wrong type", "non-existent.nested"),
		},
		{
			name:            "should error if the entityParamName has the wrong type for a nested value",
			entityParamName: "non-existent.nested",
			graphqlArgs: map[string]any{
				"non-existent": map[string]any{
					"nested": map[string]any{},
				},
			},
			expectedError: fmt.Errorf("unable to extract param from request for given paramName %q, param is of wrong type", "non-existent.nested"),
		},
		{
			name:            "should error if the entityTypeParamName is set and not part of the arguments",
			entityParamName: "existent",
			graphqlArgs: map[string]any{
				"existent": "something",
			},
			entityTypeParamName: String("non-existent"),
			expectedError:       fmt.Errorf("unable to extract param from request for given paramName %q", "non-existent"),
		},
		{
			name:            "should error if the entityType is set and but emtpy",
			entityParamName: "existent",
			entityType:      String(""),
			graphqlArgs: map[string]any{
				"existent": "something",
				"emtpy":    "",
			},
			expectedError: errors.New("make sure to either provide entityType or entityTypeParamName"),
		},
		{
			name:            "should error if the request is not allowed",
			entityParamName: "existent",
			entityType:      String("value"),
			graphqlArgs: map[string]any{
				"existent": "something",
			},
			fgaMocks: func(s *mocks.OpenFGAServiceClient) {
				s.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Name: "tenant-tenant-id",
						},
					},
				}, nil)
				s.EXPECT().ReadAuthorizationModels(mock.Anything, mock.Anything).Return(&openfgav1.ReadAuthorizationModelsResponse{
					AuthorizationModels: []*openfgav1.AuthorizationModel{
						{
							Id: "id",
						},
					},
				}, nil)
				s.EXPECT().Check(mock.Anything, mock.Anything).Return(&openfgav1.CheckResponse{Allowed: false}, nil)
			},
			expectedError: gqlerror.Errorf("unauthorized"),
		},
		{
			name:            "should allow the request and be happy",
			entityParamName: "existent",
			entityType:      String("value"),
			graphqlArgs: map[string]any{
				"existent": "something",
			},
			fgaMocks: func(s *mocks.OpenFGAServiceClient) {
				s.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Name: "tenant-tenant-id",
						},
					},
				}, nil)
				s.EXPECT().Check(mock.Anything, mock.Anything).Return(&openfgav1.CheckResponse{Allowed: true}, nil)
			},
		},
		{
			name:            "should allow the request and be happy with nested entityParamName",
			entityParamName: "nested.value",
			entityType:      String("value"),
			graphqlArgs: map[string]any{
				"nested": map[string]interface{}{
					"value": "something",
				},
			},
			fgaMocks: func(s *mocks.OpenFGAServiceClient) {
				s.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Name: "tenant-tenant-id",
						},
					},
				}, nil)
				s.EXPECT().Check(mock.Anything, mock.Anything).Return(&openfgav1.CheckResponse{Allowed: true}, nil)
			},
		},
		{
			name:            "should allow the request and be happy with super nested entityParamName",
			entityParamName: "nested.value.stuff",
			entityType:      String("value"),
			graphqlArgs: map[string]any{
				"nested": map[string]interface{}{
					"value": map[string]interface{}{
						"stuff": "test",
					},
				},
			},
			fgaMocks: func(s *mocks.OpenFGAServiceClient) {
				s.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Name: "tenant-tenant-id",
						},
					},
				}, nil)
				s.EXPECT().Check(mock.Anything, mock.Anything).Return(&openfgav1.CheckResponse{Allowed: true}, nil)
			},
		},
		{
			name:            "should allow the request and be happy for technical users",
			entityParamName: "existent",
			entityType:      String("value"),
			isTechnicalUser: true,
			graphqlArgs: map[string]any{
				"existent": "something",
				"tenantId": "tenant-id",
			},
			fgaMocks: func(s *mocks.OpenFGAServiceClient) {
				s.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Name: "tenant-tenant-id",
						},
					},
				}, nil)
				s.EXPECT().Check(mock.Anything, mock.Anything).Return(&openfgav1.CheckResponse{Allowed: true}, nil)
			},
		},
		{
			name:            "ListStores returns error",
			entityParamName: "existent",
			entityType:      String("value"),
			isTechnicalUser: true,
			graphqlArgs: map[string]any{
				"existent": "something",
				"tenantId": "tenant-id",
			},
			fgaMocks: func(s *mocks.OpenFGAServiceClient) {
				s.EXPECT().ListStores(mock.Anything, mock.Anything).Return(nil, errors.New("ListStores error"))
				// s.EXPECT().Check(mock.Anything, mock.Anything).Return(&openfgav1.CheckResponse{Allowed: true}, nil)
			},
			expectedError: errors.New("ListStores error"),
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {

			openfgaMock := mocks.NewOpenFGAServiceClient(t)
			if test.fgaMocks != nil {
				test.fgaMocks(openfgaMock)
			}

			nextFn := func(ctx context.Context) (interface{}, error) { return nil, nil }

			log := logger.New()

			ctx := context.Background()
			ctx = graphql.WithFieldContext(ctx, &graphql.FieldContext{
				Args: test.graphqlArgs,
			})

			if test.isTechnicalUser {
				ctx = pmcontext.AddIsTechnicalIssuerToContext(ctx)
			} else {
				ctx = pmcontext.AddTenantToContext(ctx, "tenant-id")
			}
			ctx = pmcontext.AddWebTokenToContext(ctx, token, signatureAlgorithms)
			ctx = pmcontext.AddAuthHeaderToContext(ctx, fmt.Sprintf("Bearer %s", token))

			_, err := Authorized(openfgaMock, log.Logger)(ctx, nil, nextFn, test.relation, test.entityType, test.entityTypeParamName, test.entityParamName)
			assert.Equal(t, test.expectedError, err)
		})
	}
}

func TestAuthorizedWithSpiffeeHeader(t *testing.T) {
	directiveConfiguration.DirectivesAuthorizationEnabled = true

	openfgaMock := mocks.NewOpenFGAServiceClient(t)

	openfgaMock.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Name: "tenant-test",
			},
		},
	}, nil)
	openfgaMock.EXPECT().ReadAuthorizationModels(mock.Anything, mock.Anything).Return(&openfgav1.ReadAuthorizationModelsResponse{
		AuthorizationModels: []*openfgav1.AuthorizationModel{
			{
				Id: "id",
			},
		},
	}, nil)
	openfgaMock.EXPECT().Check(mock.Anything, mock.Anything).Return(&openfgav1.CheckResponse{Allowed: true}, nil)

	ctx := context.Background()

	ctx = pmcontext.AddSpiffeToContext(ctx, "spiffe://cluster.local/test-spiffee")
	ctx = graphql.WithFieldContext(ctx, &graphql.FieldContext{
		Args: map[string]any{
			"tenantId":  "test",
			"projectId": "test",
		},
	})

	nextFn := func(ctx context.Context) (interface{}, error) { return nil, nil }
	_, err := Authorized(openfgaMock, logger.New().Logger)(ctx, nil, nextFn, "member", String("project"), nil, "projectId")
	assert.NoError(t, err)

}

func TestAuthorizedEdgeCases2(t *testing.T) {
	directiveConfiguration.DirectivesAuthorizationEnabled = true
	testCases := []struct {
		name                string
		relation            string
		entityID            string
		entityParamName     string
		entityType          *string
		entityTypeParamName *string
		isTechnicalUser     bool
		graphqlArgs         map[string]any
		fgaMocks            func(s *mocks.OpenFGAServiceClient)
		expectedError       error
	}{
		{
			name:            "Wrong key value for tenant in context",
			entityParamName: "existent",
			entityType:      String("value"),
			isTechnicalUser: false,
			graphqlArgs: map[string]any{
				"existent": "something",
				"tenantId": "tenant-id",
			},
			fgaMocks: func(s *mocks.OpenFGAServiceClient) {
			},
			expectedError: fmt.Errorf("someone stored a wrong value in the [tenantId] key with type [<nil>], expected [string]"),
		},
		{
			name:            "Check() return error",
			entityParamName: "existent",
			entityType:      String("value"),
			isTechnicalUser: true,
			graphqlArgs: map[string]any{
				"existent": "something",
				"tenantId": "tenant-id",
			},
			fgaMocks: func(s *mocks.OpenFGAServiceClient) {
				s.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Name: "tenant-tenant-id",
						},
					},
				}, nil)
				s.EXPECT().Check(mock.Anything, mock.Anything).Return(nil, errors.New("Check error"))
			},
			expectedError: errors.New("Check error"),
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {

			openfgaMock := mocks.NewOpenFGAServiceClient(t)
			if test.fgaMocks != nil {
				test.fgaMocks(openfgaMock)
			}

			nextFn := func(ctx context.Context) (interface{}, error) { return nil, nil }

			log := logger.New()

			ctx := context.Background()
			ctx = graphql.WithFieldContext(ctx, &graphql.FieldContext{
				Args: test.graphqlArgs,
			})

			if test.isTechnicalUser {
				ctx = pmcontext.AddIsTechnicalIssuerToContext(ctx)
			} else {
				ctx = context.WithValue(ctx, keys.WebTokenCtxKey, "tenantId")
			}
			ctx = pmcontext.AddWebTokenToContext(ctx, token, signatureAlgorithms)
			ctx = pmcontext.AddAuthHeaderToContext(ctx, fmt.Sprintf("Bearer %s", token))

			_, err := Authorized(openfgaMock, log.Logger)(ctx, nil, nextFn, test.relation, test.entityType, test.entityTypeParamName, test.entityParamName)
			assert.Equal(t, test.expectedError, err)
		})
	}

}

func TestAuthorizedEdgeCases(t *testing.T) {
	directiveConfiguration.DirectivesAuthorizationEnabled = false
	testCases := []struct {
		name                string
		relation            string
		entityID            string
		entityParamName     string
		entityType          *string
		entityTypeParamName *string
		isTechnicalUser     bool
		graphqlArgs         map[string]any
		fgaMocks            func(s *mocks.OpenFGAServiceClient)
		expectedError       error
	}{
		{
			name:            "should go to next() due to disabled directives configuration",
			entityParamName: "existent",
			entityType:      String("value"),
			isTechnicalUser: true,
			graphqlArgs: map[string]any{
				"existent": "something",
				"tenantId": "tenant-id",
			},
			fgaMocks: nil,
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {

			openfgaMock := mocks.NewOpenFGAServiceClient(t)
			if test.fgaMocks != nil {
				test.fgaMocks(openfgaMock)
			}

			nextFn := func(ctx context.Context) (interface{}, error) { return nil, nil }

			log := logger.New()

			ctx := context.Background()
			ctx = graphql.WithFieldContext(ctx, &graphql.FieldContext{
				Args: test.graphqlArgs,
			})

			if test.isTechnicalUser {
				ctx = pmcontext.AddIsTechnicalIssuerToContext(ctx)
			} else {
				ctx = pmcontext.AddTenantToContext(ctx, "tenant-id")
			}
			ctx = pmcontext.AddWebTokenToContext(ctx, token, signatureAlgorithms)
			ctx = pmcontext.AddAuthHeaderToContext(ctx, fmt.Sprintf("Bearer %s", token))

			_, err := Authorized(openfgaMock, log.Logger)(ctx, nil, nextFn, test.relation, test.entityType, test.entityTypeParamName, test.entityParamName)
			assert.Equal(t, test.expectedError, err)
		})
	}

	// test 2
	directiveConfiguration.DirectivesAuthorizationEnabled = true
	testCases = []struct {
		name                string
		relation            string
		entityID            string
		entityParamName     string
		entityType          *string
		entityTypeParamName *string
		isTechnicalUser     bool
		graphqlArgs         map[string]any
		fgaMocks            func(s *mocks.OpenFGAServiceClient)
		expectedError       error
	}{
		{
			name:            "should go to next() due to disabled directives configuration",
			entityParamName: "existent",
			entityType:      String("value"),
			isTechnicalUser: true,
			graphqlArgs: map[string]any{
				"existent": "something",
				"tenantId": "tenant-id",
			},
			fgaMocks: func(s *mocks.OpenFGAServiceClient) {
				s.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Name: "tenant-tenant-id",
						},
					},
				}, nil)
				s.EXPECT().ReadAuthorizationModels(mock.Anything, mock.Anything).Return(&openfgav1.ReadAuthorizationModelsResponse{
					AuthorizationModels: []*openfgav1.AuthorizationModel{
						{
							Id: "id",
						},
					},
				}, nil)
				s.EXPECT().Check(mock.Anything, mock.Anything).Return(&openfgav1.CheckResponse{Allowed: false}, nil)
			},
			expectedError: errors.New("OpenFGAServiceClient is nil. Cannot process request"),
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {

			nextFn := func(ctx context.Context) (interface{}, error) { return nil, nil }

			log := logger.New()

			ctx := context.Background()
			ctx = graphql.WithFieldContext(ctx, &graphql.FieldContext{
				Args: test.graphqlArgs,
			})

			if test.isTechnicalUser {
				ctx = pmcontext.AddIsTechnicalIssuerToContext(ctx)
			} else {
				ctx = pmcontext.AddTenantToContext(ctx, "tenant-id")
			}
			ctx = pmcontext.AddWebTokenToContext(ctx, token, signatureAlgorithms)
			ctx = pmcontext.AddAuthHeaderToContext(ctx, fmt.Sprintf("Bearer %s", token))

			_, err := Authorized(nil, log.Logger)(ctx, nil, nextFn, test.relation, test.entityType, test.entityTypeParamName, test.entityParamName)
			assert.Equal(t, test.expectedError.Error(), err.Error())
		})
	}

}
