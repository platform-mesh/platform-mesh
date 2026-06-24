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
	"strconv"
	"sync"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"

	pmcontext "go.platform-mesh.io/golang-commons/context"
)

const iamUrl = "http://localhost/is/mocked/in/these/tests"

type mockTenantReader struct {
	tenantId  string
	error     error
	callCount int
}

func (mc *mockTenantReader) Read(_ context.Context) (string, error) {
	mc.callCount = mc.callCount + 1

	if mc.error != nil {
		return "", mc.error
	}

	return mc.tenantId, nil
}

func TestTenantRetriever(t *testing.T) {
	tenantId := "01emp2m3v3batersxj73qhm5zq"
	issuer := "https://idp.accounts.com"
	audience := "audience"

	t.Run("With a false token - empty response", func(t *testing.T) {
		retriever, mockClient := createRetrieverWithMock()

		t.Run("no token in the request", func(t *testing.T) {
			id, err := retriever.RetrieveTenant(context.Background())

			assert.NoError(t, err)
			assert.Equal(t, "", id)
			assert.Equal(t, 0, mockClient.callCount)
		})

		t.Run("no issuer in the token", func(t *testing.T) {
			claims := &jwt.RegisteredClaims{Audience: jwt.ClaimStrings{audience}}
			token, _ := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)

			ctx := pmcontext.AddAuthHeaderToContext(context.Background(), fmt.Sprintf("Bearer %s", token))

			id, err := retriever.RetrieveTenant(ctx)

			assert.NoError(t, err)
			assert.Equal(t, "", id)
			assert.Equal(t, 0, mockClient.callCount)
		})

		t.Run("no audience in the token", func(t *testing.T) {
			claims := &jwt.RegisteredClaims{Issuer: issuer}
			token, _ := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)

			ctx := pmcontext.AddAuthHeaderToContext(context.Background(), fmt.Sprintf("Bearer %s", token))

			id, err := retriever.RetrieveTenant(ctx)

			assert.NoError(t, err)
			assert.Equal(t, "", id)
			assert.Equal(t, 0, mockClient.callCount)
		})
	})

	t.Run("With a mocked client", func(t *testing.T) {
		retriever, mockClient := createRetrieverWithMock()
		mockClient.tenantId = tenantId

		t.Run("gets a tenant from a mocked client", func(t *testing.T) {
			testContext := context.Background()

			// Act
			claims := &jwt.RegisteredClaims{Issuer: issuer, Audience: jwt.ClaimStrings{audience}}
			token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
			assert.NoError(t, err)
			testContext = pmcontext.AddWebTokenToContext(testContext, token, []jose.SignatureAlgorithm{jose.SignatureAlgorithm("none")})
			testContext = pmcontext.AddAuthHeaderToContext(testContext, fmt.Sprintf("Bearer %s", token))
			id, err := retriever.RetrieveTenant(testContext)

			// Assert
			assert.NoError(t, err)
			assert.Equal(t, tenantId, id)
			assert.Equal(t, 1, mockClient.callCount)
		})

		t.Run("caches the call and returns the cached tenant id", func(t *testing.T) {
			testContext := context.Background()

			// Act
			claims := &jwt.RegisteredClaims{Issuer: issuer, Audience: jwt.ClaimStrings{audience}}
			token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
			assert.NoError(t, err)
			testContext = pmcontext.AddWebTokenToContext(testContext, token, []jose.SignatureAlgorithm{jose.SignatureAlgorithm("none")})
			testContext = pmcontext.AddAuthHeaderToContext(testContext, fmt.Sprintf("Bearer %s", token))

			id1, err1 := retriever.RetrieveTenant(testContext)
			id2, err2 := retriever.RetrieveTenant(testContext)

			// Assert
			assert.NoError(t, err1)
			assert.NoError(t, err2)
			assert.Equal(t, tenantId, id1)
			assert.Equal(t, tenantId, id2)
			assert.Equal(t, 1, mockClient.callCount)
		})
	})

	t.Run("With a client that throws an error", func(t *testing.T) {
		testContext := context.Background()

		// Arrange
		claims := &jwt.RegisteredClaims{Issuer: issuer, Audience: jwt.ClaimStrings{audience}}
		token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
		assert.NoError(t, err)
		testContext = pmcontext.AddWebTokenToContext(testContext, token, []jose.SignatureAlgorithm{jose.SignatureAlgorithm("none")})
		testContext = pmcontext.AddAuthHeaderToContext(testContext, fmt.Sprintf("Bearer %s", token))
		retriever, mockClient := createRetrieverWithMock()
		errMsg := "oh nose"
		mockedErr := errors.New(errMsg)
		mockClient.error = mockedErr

		// Act
		id, err := retriever.RetrieveTenant(testContext)

		// Assert
		assert.EqualError(t, err, errMsg)
		assert.Equal(t, "", id)
		assert.Equal(t, 1, mockClient.callCount)
	})

	t.Run("With concurrency", func(t *testing.T) {
		t.Run("has no problems with parallel reads/writes", func(t *testing.T) {
			var wg sync.WaitGroup

			// create a single retriever with a cache
			retriever, mockClient := createRetrieverWithMock()

			// simulate multiple requests filling/reading the cache with the same values
			for i := 0; i < 9; i++ {
				wg.Add(1)
				go func(in int) {
					defer wg.Done()
					mockClient.tenantId = tenantId + strconv.Itoa(in%3)

					claims := &jwt.RegisteredClaims{Issuer: issuer, Audience: jwt.ClaimStrings{audience + strconv.Itoa(in%3)}}
					token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
					assert.NoError(t, err)
					ctx := pmcontext.AddAuthHeaderToContext(context.Background(), fmt.Sprintf("Bearer %s", token))

					_, _ = retriever.RetrieveTenant(ctx)
				}(i)
			}

			wg.Wait()
			// no crash
		})
	})

	t.Run("Cache - without a client", func(t *testing.T) {
		r := NewTenantRetriever(context.Background(), iamUrl, nil)
		// should not use the client
		r.tenantReader = nil

		t.Run("With an empty issuer - returns an empty tenant", func(t *testing.T) {
			testContext := context.Background()

			// Arrange
			issuer := ""
			claims := &jwt.RegisteredClaims{Issuer: issuer, Audience: jwt.ClaimStrings{audience}}
			token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
			assert.NoError(t, err)
			testContext = pmcontext.AddWebTokenToContext(testContext, token, []jose.SignatureAlgorithm{jose.SignatureAlgorithm("none")})
			testContext = pmcontext.AddAuthHeaderToContext(testContext, fmt.Sprintf("Bearer %s", token))

			// Act
			id, err := r.RetrieveTenant(testContext)
			// Assert
			assert.NoError(t, err)
			assert.Equal(t, id, "")
		})

		t.Run("With a issuer/audience already in the cache", func(t *testing.T) {
			testContext := context.Background()

			// Arrange
			key := fmt.Sprintf("%s-%s", issuer, audience)
			r.tenantCache.Add(TenantKey(key), tenantId)

			claims := &jwt.RegisteredClaims{Issuer: issuer, Audience: jwt.ClaimStrings{audience}}
			token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
			assert.NoError(t, err)
			testContext = pmcontext.AddWebTokenToContext(testContext, token, []jose.SignatureAlgorithm{jose.SignatureAlgorithm("none")})
			testContext = pmcontext.AddAuthHeaderToContext(testContext, fmt.Sprintf("Bearer %s", token))

			// Act
			id, err := r.RetrieveTenant(testContext)

			// Assert
			assert.NoError(t, err)
			assert.Equal(t, tenantId, id)
		})
	})
}

func createRetrieverWithMock() (*TenantRetrieverService, *mockTenantReader) {
	mockClient := &mockTenantReader{}
	retriever := NewCustomTenantRetriever(mockClient)
	return retriever, mockClient
}
