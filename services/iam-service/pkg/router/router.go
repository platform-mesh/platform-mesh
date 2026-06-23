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

package router

import (
	"context"
	"net/http"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/go-chi/chi/v5"
	"github.com/vektah/gqlparser/v2/ast"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"

	"go.platform-mesh.io/iam-service/pkg/config"
	"go.platform-mesh.io/iam-service/pkg/graph"
	"go.platform-mesh.io/iam-service/pkg/metrics"
)

func CreateRouter(
	commonCfg *pmconfig.CommonServiceConfig,
	serviceConfig *config.ServiceConfig,
	res graph.ResolverRoot,
	log *logger.Logger,
	mws []func(http.Handler) http.Handler,
	ad graph.DirectiveRoot,
) *chi.Mux {
	router := chi.NewRouter()

	gql := graph.Config{
		Resolvers: res,
	}

	gql.Directives = ad
	gqHandler := handler.New(graph.NewExecutableSchema(gql))

	gqHandler.AddTransport(transport.Options{})
	gqHandler.AddTransport(transport.GET{})
	gqHandler.AddTransport(transport.POST{})

	gqHandler.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	gqHandler.Use(extension.Introspection{})
	gqHandler.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100),
	})

	gqHandler.AroundOperations(func(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
		oc := graphql.GetOperationContext(ctx)
		opName := oc.OperationName
		if opName == "" {
			opName = "unknown"
		}
		rh := next(ctx)
		return func(ctx context.Context) *graphql.Response {
			resp := rh(ctx)
			result := "success"
			if resp != nil && len(resp.Errors) > 0 {
				result = "error"
			}
			metrics.GraphQLRequests.WithLabelValues(opName, result).Inc()
			return resp
		}
	})

	if commonCfg.IsLocal {
		router.Handle("/", playground.Handler("GraphQL playground", "/graphql"))
	}

	router.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	router.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	router.With(mws...).Handle("/graphql", gqHandler)
	return router
}
