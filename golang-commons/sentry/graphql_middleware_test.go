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

package sentry

import (
	"context"
	"errors"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/jwt"
	"go.platform-mesh.io/golang-commons/logger"
	testlogger "go.platform-mesh.io/golang-commons/logger/testlogger"
)

func TestGraphQLRecover(t *testing.T) {
	// Given
	log := testlogger.New()
	recoverFunc := GraphQLRecover(log.Logger)
	ctx := context.WithValue(context.Background(), pmcontext.ContextKey(jwt.TenantIdCtxKey), "test")
	ctx = graphql.WithOperationContext(ctx, &graphql.OperationContext{
		Operation: &ast.OperationDefinition{
			Name:      "test",
			Operation: ast.Query,
		},
	})
	ctx = graphql.WithPathContext(ctx, &graphql.PathContext{
		ParentField: &graphql.FieldContext{
			Field: graphql.CollectedField{
				Field: &ast.Field{
					Alias: "test",
					Name:  "test",
				},
			},
		},
	})

	// When
	err := recoverFunc(ctx, "test error")

	// Then
	assert.Equal(t, gqlerror.Errorf("internal server error: test error"), err)
}

func TestGraphQLErrorPresenter(t *testing.T) {
	//Given
	presenter := GraphQLErrorPresenter()
	testError := errors.New("test error")
	ctx := pmcontext.AddTenantToContext(context.Background(), "test")

	//When
	err := presenter(ctx, testError)

	//Then
	expectedErr := gqlerror.Wrap(testError)
	assert.Equal(t, expectedErr, err)
}

func TestGraphQLErrorPresenterNilError(t *testing.T) {
	//Given
	presenter := GraphQLErrorPresenter()
	var testError error
	ctx := context.Background()

	//When
	err := presenter(ctx, testError)

	//Then
	assert.Nil(t, err)
}

func TestGraphQLErrorPresenterWithoutTenantContext(t *testing.T) {
	presenter := GraphQLErrorPresenter()
	testError := SentryError(errors.New("test error"))
	ctx := context.Background()

	//When
	err := presenter(ctx, testError)

	//Then
	expectedErr := gqlerror.Wrap(testError)
	assert.Equal(t, expectedErr, err)
}

func TestGraphQLErrorPresenterWithSkipTenants(t *testing.T) {
	//Given
	presenter := GraphQLErrorPresenter("test")
	testError := SentryError(errors.New("test error"))
	tl := testlogger.New()
	ctx := pmcontext.AddTenantToContext(context.Background(), "test")
	ctx = logger.SetLoggerInContext(ctx, tl.Logger)

	//When
	err := presenter(ctx, testError)

	//Then
	expectedErr := gqlerror.Wrap(testError)
	assert.Equal(t, expectedErr, err)

	messages, err2 := tl.GetLogMessages()
	assert.NoError(t, err2)
	assert.Len(t, messages, 1)
	assert.Equal(t, "Error not sent to Sentry for skipped tenant", messages[0].Message)
}
