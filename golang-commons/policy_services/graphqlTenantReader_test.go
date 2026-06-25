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
	"errors"
	"fmt"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/machinebox/graphql"
	"github.com/stretchr/testify/assert"

	pmcontext "go.platform-mesh.io/golang-commons/context"
)

type graphMockClient struct {
	tenantId    string
	tenantError error
	callCount   int
}

func (mc *graphMockClient) Run(_ context.Context, _ *graphql.Request, resp any) error {
	mc.callCount = mc.callCount + 1

	if mc.tenantError != nil {
		return mc.tenantError
	}

	data := resp.(*GraphqlData)
	data.TenantInfo.TenantId = mc.tenantId

	return nil
}

func TestTenantReader(t *testing.T) {
	t.Run("returns an error without a jwt", func(t *testing.T) {
		// Arrange
		retriever, mockClient := createReaderWithMock()

		// Act
		id, err := retriever.Read(context.Background())

		// Assert
		assert.EqualError(t, err, "the request context does not contain an auth header under the key \"authHeader\". You can use authz.context to set it")
		assert.Equal(t, "", id)
		assert.Equal(t, 0, mockClient.callCount)
	})

	t.Run("With a jwt in the context", func(t *testing.T) {
		claims := &jwt.RegisteredClaims{Issuer: "an issuer", Audience: jwt.ClaimStrings{"an audience"}}
		token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
		assert.NoError(t, err)

		ctx := pmcontext.AddAuthHeaderToContext(context.Background(), fmt.Sprintf("Bearer %s", token))
		ctx = pmcontext.AddWebTokenToContext(ctx, token, []jose.SignatureAlgorithm{jose.SignatureAlgorithm("none")})

		t.Run("gets a tenant from a mocked client", func(t *testing.T) {
			// Arrange
			tenantId := "01emp2m3v3batersxj73qhm5zq"
			reader, mockClient := createReaderWithMock()
			mockClient.tenantId = tenantId

			// Act
			id, err := reader.Read(ctx)

			// Assert
			assert.NoError(t, err)
			assert.Equal(t, tenantId, id)
			assert.Equal(t, 1, mockClient.callCount)
		})

		t.Run("returns an error for an empty tenantId", func(t *testing.T) {
			// Arrange
			retriever, mockClient := createReaderWithMock()
			retriever.iamUrl = "some-iam-url"

			// Act
			id, err := retriever.Read(ctx)

			// Assert
			assert.EqualError(t, err, "the tenantInfo query returned no tenant id. The iam service some-iam-url was called")
			assert.Equal(t, "", id)
			assert.Equal(t, 1, mockClient.callCount)
		})

		t.Run("returns the error if the client fails", func(t *testing.T) {
			// Arrange
			errorMessage := "oh nose"
			retriever, mockClient := createReaderWithMock()
			mockClient.tenantError = errors.New(errorMessage)

			// Act
			id, err := retriever.Read(ctx)

			// Assert
			assert.EqualError(t, err, errorMessage)
			assert.Equal(t, "", id)
			assert.Equal(t, 1, mockClient.callCount)
		})
	})
}

func createReaderWithMock() (*graphqlTenantReader, *graphMockClient) {
	r := &graphqlTenantReader{}
	mockClient := &graphMockClient{}
	r.client = mockClient
	return r, mockClient
}
