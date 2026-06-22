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
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"

	pmcontext "go.platform-mesh.io/golang-commons/context"
)

func TestLocalTenantReader(t *testing.T) {
	t.Run("gets a tenant id", func(t *testing.T) {
		testContext := context.Background()

		// Arrange
		tenantId := "01emp2m3v3batersxj73qhm5zq"
		reader := NewLocalTenantRetriever(tenantId)

		claims := &jwt.RegisteredClaims{Issuer: "an issuer", Audience: jwt.ClaimStrings{"an audience"}}
		token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
		assert.NoError(t, err)

		testContext = pmcontext.AddWebTokenToContext(testContext, token, []jose.SignatureAlgorithm{jose.SignatureAlgorithm("none")})
		testContext = pmcontext.AddAuthHeaderToContext(testContext, fmt.Sprintf("Bearer %s", token))

		// Act
		id, err := reader.RetrieveTenant(testContext)

		// Assert
		assert.NoError(t, err)
		assert.Equal(t, tenantId, id)
	})
}
