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
	"net/http"
	"time"

	"github.com/machinebox/graphql"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/jwt"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/middleware"
)

func createClient(ctx context.Context, iamApiUrl string) *graphql.Client {
	log := logger.LoadLoggerFromContext(ctx)

	hc := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	client := graphql.NewClient(iamApiUrl, graphql.WithHTTPClient(hc))

	if log != nil {
		client.Log = func(msg string) {
			log.ComponentLogger("graphql").Trace().Msg(msg)
		}
	}
	return client
}

func run(ctx context.Context, client GraphqlClient, request *graphql.Request, resp any, timeout time.Duration) error {
	auth, err := pmcontext.GetAuthHeaderFromContext(ctx)
	if err != nil || len(auth) == 0 {
		return fmt.Errorf("the request context does not contain an auth header under the key %q. You can use authz.context to set it", jwt.AuthHeaderCtxKey)
	}
	request.Header.Add(middleware.AuthorizationHeader, auth)
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return client.Run(requestCtx, request, resp)
}
