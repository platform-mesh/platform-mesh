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

package graphql

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/handler"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/config"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// GraphQLServer provides utility methods for creating GraphQL handlers.
type GraphQLServer struct {
	config config.GraphQL
}

// NewGraphQLServer creates a new GraphQL server.
func NewGraphQLServer(cfg config.GraphQL) *GraphQLServer {
	return &GraphQLServer{
		config: cfg,
	}
}

// GraphQLHandler wraps a GraphQL schema and its HTTP handler.
type GraphQLHandler struct {
	Schema  *graphql.Schema
	Handler http.Handler
}

// CreateHandler creates a new GraphQL handler from a schema.
func (s *GraphQLServer) CreateHandler(schema *graphql.Schema) *GraphQLHandler {
	graphqlHandler := handler.New(&handler.Config{
		Schema:     schema,
		Pretty:     s.config.Pretty,
		Playground: s.config.PlaygroundEnabled,
		GraphiQL:   s.config.GraphiQL,
	})
	return &GraphQLHandler{
		Schema:  schema,
		Handler: graphqlHandler,
	}
}

// HandleSubscription handles GraphQL subscription requests using Server-Sent Events.
func (s *GraphQLServer) HandleSubscription(w http.ResponseWriter, r *http.Request, schema *graphql.Schema) {
	logger := log.FromContext(r.Context())

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var params struct {
		Query         string         `json:"query"`
		OperationName string         `json:"operationName"`
		Variables     map[string]any `json:"variables"`
	}

	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "Error parsing JSON request body", http.StatusBadRequest)
		return
	}

	flusher := http.NewResponseController(w)

	if err := r.Body.Close(); err != nil {
		logger.V(4).Error(err, "Failed to close request body")
	}

	subscriptionParams := graphql.Params{
		Schema:         *schema,
		RequestString:  params.Query,
		VariableValues: params.Variables,
		OperationName:  params.OperationName,
		Context:        r.Context(),
	}

	if err := flusher.Flush(); err != nil {
		logger.V(4).Error(err, "Failed to flush initial SSE response")
		return
	}

	subscriptionChannel := graphql.Subscribe(subscriptionParams)
	for res := range subscriptionChannel {
		if res == nil {
			continue
		}

		data, err := json.Marshal(res)
		if err != nil {
			logger.Error(err, "Error marshalling subscription response")
			continue
		}

		if _, err := fmt.Fprintf(w, "event: next\ndata: %s\n\n", data); err != nil {
			logger.V(4).Error(err, "Failed to write SSE event")
			return
		}

		if err := flusher.Flush(); err != nil {
			logger.V(4).Error(err, "Failed to flush SSE response")
			return
		}
	}

	// Only send the complete event if the client is still connected.
	select {
	case <-r.Context().Done():
	default:
		if _, err := fmt.Fprint(w, "event: complete\n\n"); err != nil {
			logger.V(4).Error(err, "Failed to write SSE complete event")
		}
	}
}
