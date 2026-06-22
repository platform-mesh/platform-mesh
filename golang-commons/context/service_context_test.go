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

package context_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/context/keys"
	pmjwt "go.platform-mesh.io/golang-commons/jwt"
)

type astruct struct{}

var signatureAlgorithms = []jose.SignatureAlgorithm{jose.HS256}

func TestAddSpiffeToContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = pmcontext.AddSpiffeToContext(ctx, "spiffe")

	spiffe, err := pmcontext.GetSpiffeFromContext(ctx)
	assert.Nil(t, err)
	assert.Equal(t, "spiffe", spiffe)
}

func TestWrongSpiffeToContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	key := pmcontext.ContextKey(pmjwt.SpiffeCtxKey)
	ctx = context.WithValue(ctx, key, astruct{})

	_, err := pmcontext.GetSpiffeFromContext(ctx)
	assert.Error(t, err, "someone stored a wrong value in the [spiffe] key with type [context.astruct], expected [string]")
}

func TestAddTenantToContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = pmcontext.AddTenantToContext(ctx, "tenant")

	tenant, err := pmcontext.GetTenantFromContext(ctx)
	assert.Nil(t, err)
	assert.Equal(t, "tenant", tenant)
}

func TestAddTenantToContextNegative(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	key := pmcontext.ContextKey(pmjwt.TenantIdCtxKey)
	ctx = context.WithValue(ctx, key, astruct{})

	_, err := pmcontext.GetTenantFromContext(ctx)
	assert.Error(t, err, "someone stored a wrong value in the [tenant] key with type [context.astruct], expected [string]")
}

func TestAddAndGetAuthHeaderToContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		authHeader  any
		expectError bool
	}{
		{
			name:        "should return the auth header from if stored in the context",
			authHeader:  "auth",
			expectError: false,
		},
		{
			name:        "should error out if a wrong value is stored inside the context",
			authHeader:  4,
			expectError: true,
		},
		{
			name:        "should error out if no value is stored inside the context",
			authHeader:  nil,
			expectError: true,
		},
		{
			name:        "should error out if an empty string is stored inside the context",
			authHeader:  "",
			expectError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), keys.AuthHeaderCtxKey, test.authHeader)

			val, err := pmcontext.GetAuthHeaderFromContext(ctx)
			if test.expectError {
				assert.Error(t, err)
				return
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, test.authHeader, val)

		})
	}
}

func TestAddWebTokenToContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	issuer := "my-issuer"
	tokenString, err := generateJWT(issuer)
	assert.NoError(t, err)

	ctx = pmcontext.AddWebTokenToContext(ctx, tokenString, signatureAlgorithms)

	token, err := pmcontext.GetWebTokenFromContext(ctx)
	assert.Nil(t, err)
	assert.Equal(t, issuer, token.Issuer)
}

func TestAddWebTokenToContextNegative(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, nil)

	_, err := pmcontext.GetWebTokenFromContext(ctx)
	assert.ErrorContains(t, err, "someone stored a wrong value in the [webToken] key with type [<nil>], expected [jwt.WebToken]")
}

func TestAddWebTokenToContextWrongToken(t *testing.T) {
	t.Parallel()

	initialContext := context.Background()
	tokenString := "not-a-token"

	ctx := pmcontext.AddWebTokenToContext(initialContext, tokenString, signatureAlgorithms)

	assert.Equal(t, initialContext, ctx)
}

func generateJWT(issuer string) (string, error) {
	claims := &jwt.RegisteredClaims{
		Issuer: issuer,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("a_secret_key"))

	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func TestAddIsTechnicalIssuerToContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = pmcontext.AddIsTechnicalIssuerToContext(ctx)

	isTechnicalIssuer := pmcontext.GetIsTechnicalIssuerFromContext(ctx)
	assert.True(t, isTechnicalIssuer)
}

func TestAddIsTechnicalIssuerToContextNegative(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	isTechnicalIssuer := pmcontext.GetIsTechnicalIssuerFromContext(ctx)
	assert.False(t, isTechnicalIssuer)
}

func TestHasTenantInContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = pmcontext.AddTenantToContext(ctx, "tenant")

	hasTenant := pmcontext.HasTenantInContext(ctx)
	assert.True(t, hasTenant)
}

func TestHasTenantInContextNegative(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	hasTenant := pmcontext.HasTenantInContext(ctx)
	assert.False(t, hasTenant)
}

func TestAddUserIDToContextAndGetUserIDFromContext(t *testing.T) {
	baseCtx := context.Background()
	userID := "testUser123"

	ctxWithUserID := pmcontext.AddUserIDToContext(baseCtx, userID)

	retrievedUserID, err := pmcontext.GetUserIDFromContext(ctxWithUserID)
	assert.NoError(t, err, "Expected no error when retrieving userID")
	assert.Equal(t, userID, retrievedUserID, "Retrieved userID should match the added value")
}

func TestGetUserIDFromContextWrongType(t *testing.T) {
	baseCtx := context.Background()

	ctxWithWrongType := context.WithValue(baseCtx, keys.UserIDCtxKey, 123)

	retrievedUserID, err := pmcontext.GetUserIDFromContext(ctxWithWrongType)
	assert.Error(t, err, "Expected an error when retrieving userID with the wrong type")
	expectedErrorMsg := fmt.Sprintf("someone stored a wrong value in the [%s] key with type [%T], expected [string]", keys.UserIDCtxKey, ctxWithWrongType.Value(keys.UserIDCtxKey))
	assert.Equal(t, expectedErrorMsg, err.Error(), "Error message should match the expected message")
	assert.Equal(t, "", retrievedUserID, "Retrieved userID should be an empty string when an error occurs")
}

func TestHasUserIDInContext(t *testing.T) {
	baseCtx := context.Background()

	assert.False(t, pmcontext.HasUserIDInContext(baseCtx), "Expected false when userID is not set in context")

	ctxWithUserID := pmcontext.AddUserIDToContext(baseCtx, "user123")
	assert.True(t, pmcontext.HasUserIDInContext(ctxWithUserID), "Expected true when a valid userID is set in context")

	ctxWithWrongType := context.WithValue(baseCtx, keys.UserIDCtxKey, 456)
	assert.False(t, pmcontext.HasUserIDInContext(ctxWithWrongType), "Expected false when the value stored is of the wrong type")
}
