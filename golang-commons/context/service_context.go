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

package context

import (
	"context"
	"fmt"

	"github.com/go-jose/go-jose/v4"

	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/jwt"
	"go.platform-mesh.io/golang-commons/logger"
)

type ContextKey string

func AddSpiffeToContext(ctx context.Context, spiffe string) context.Context {
	return context.WithValue(ctx, keys.SpiffeCtxKey, spiffe)
}

func GetSpiffeFromContext(ctx context.Context) (string, error) {
	spiffe, ok := ctx.Value(keys.SpiffeCtxKey).(string)
	if !ok {
		return spiffe, fmt.Errorf("someone stored a wrong value in the [%s] key with type [%T], expected [string]", jwt.SpiffeCtxKey, ctx.Value(keys.SpiffeCtxKey))
	}

	return spiffe, nil
}

func AddTenantToContext(ctx context.Context, tenantId string) context.Context {
	return context.WithValue(ctx, keys.TenantIdCtxKey, tenantId)
}

func GetTenantFromContext(ctx context.Context) (string, error) {
	tenantId, ok := ctx.Value(keys.TenantIdCtxKey).(string)
	if !ok {
		return tenantId, fmt.Errorf("someone stored a wrong value in the [%s] key with type [%T], expected [string]", jwt.TenantIdCtxKey, ctx.Value(keys.TenantIdCtxKey))
	}

	return tenantId, nil
}

func HasTenantInContext(ctx context.Context) bool {
	_, ok := ctx.Value(keys.TenantIdCtxKey).(string)
	return ok
}

func AddAuthHeaderToContext(ctx context.Context, headerValue string) context.Context {
	return context.WithValue(ctx, keys.AuthHeaderCtxKey, headerValue)
}

func GetAuthHeaderFromContext(ctx context.Context) (string, error) {
	auth, ok := ctx.Value(keys.AuthHeaderCtxKey).(string)
	if !ok || auth == "" {
		return auth, fmt.Errorf("someone stored a wrong or empty value in the [%s] key with type [%T], expected [string]", jwt.AuthHeaderCtxKey, ctx.Value(keys.AuthHeaderCtxKey))
	}

	return auth, nil
}

func AddWebTokenToContext(ctx context.Context, idToken string, signatureAlgorithms []jose.SignatureAlgorithm) context.Context {
	token, err := jwt.New(idToken, signatureAlgorithms)
	if err != nil {
		logger.StdLogger.Error().Err(err).Msg("cannot add given id_token to context")
		return ctx
	}
	return context.WithValue(ctx, keys.WebTokenCtxKey, token)
}

func GetWebTokenFromContext(ctx context.Context) (jwt.WebToken, error) {
	idToken, ok := ctx.Value(keys.WebTokenCtxKey).(jwt.WebToken)
	if !ok {
		return idToken, fmt.Errorf("someone stored a wrong value in the [%s] key with type [%T], expected [jwt.WebToken]", jwt.WebTokenCtxKey, ctx.Value(keys.WebTokenCtxKey))
	}

	return idToken, nil
}

func AddIsTechnicalIssuerToContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, keys.TechnicalUserCtxKey, true)
}

func GetIsTechnicalIssuerFromContext(ctx context.Context) bool {
	isTechnicalIsser, ok := ctx.Value(keys.TechnicalUserCtxKey).(bool)
	if !ok {
		return false
	}

	return isTechnicalIsser
}

func AddUserIDToContext(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, keys.UserIDCtxKey, userID)
}

func GetUserIDFromContext(ctx context.Context) (string, error) {
	userID, ok := ctx.Value(keys.UserIDCtxKey).(string)
	if !ok {
		return userID, fmt.Errorf("someone stored a wrong value in the [%s] key with type [%T], expected [string]", keys.UserIDCtxKey, ctx.Value(keys.UserIDCtxKey))
	}
	return userID, nil
}

func HasUserIDInContext(ctx context.Context) bool {
	_, ok := ctx.Value(keys.UserIDCtxKey).(string)
	return ok
}
