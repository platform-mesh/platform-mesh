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

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/gqlerror"

	pmpcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/logger"
)

func setTenantToContextForTechnicalUsers(ctx context.Context, l *logger.Logger) (context.Context, error) {
	spiffee, err := pmpcontext.GetSpiffeFromContext(ctx)
	hasSpiffee := err == nil && spiffee != ""
	if isTechnicalIssuer := pmpcontext.GetIsTechnicalIssuerFromContext(ctx); !isTechnicalIssuer && !hasSpiffee {
		return ctx, nil
	}

	fieldContext := graphql.GetFieldContext(ctx)
	var tenantID string
	switch tID := fieldContext.Args["tenantId"].(type) {
	case string:
		tenantID = tID
	case *string:
		if tID == nil {
			return nil, &gqlerror.Error{Message: "tenantId parameter is nil - bad request"}
		}
		tenantID = *tID
	}

	if tenantID == "" {
		return ctx, nil
	}

	ctx = pmpcontext.AddTenantToContext(ctx, tenantID)
	l.Debug().Str("tenantId", tenantID).Msg("Added a tenant id for technical user to the context")
	return ctx, nil
}
