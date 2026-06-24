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

package roundtripper

import (
	"net/http"

	utilscontext "go.platform-mesh.io/kubernetes-graphql-gateway/gateway/utils/context"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// BearerHandler extracts a bearer token from the request context and adds it
// to the Authorization header. Returns 401 Unauthorized if no token is found.
// This is a terminal handler - it always handles the request.
type BearerHandler struct {
	baseRT         http.RoundTripper
	unauthorizedRT http.RoundTripper
}

// NewBearerHandler creates a handler that injects bearer tokens from context.
func NewBearerHandler(baseRT, unauthorizedRT http.RoundTripper) *BearerHandler {
	return &BearerHandler{
		baseRT:         baseRT,
		unauthorizedRT: unauthorizedRT,
	}
}

// RoundTrip implements union.Handler.
func (h *BearerHandler) RoundTrip(req *http.Request) (*http.Response, error, bool) {
	ctx := req.Context()
	logger := log.FromContext(ctx)

	token, ok := utilscontext.GetTokenFromCtx(ctx)
	if !ok || token == "" {
		logger.V(4).WithValues("path", req.URL.Path).Error(nil, "No token found for resource request, denying")
		resp, err := h.unauthorizedRT.RoundTrip(req)
		return resp, err, true
	}

	req = utilnet.CloneRequest(req)
	req.Header.Set("Authorization", "Bearer "+token)

	logger.V(4).WithValues("path", req.URL.Path).Info("Using bearer token authentication")
	resp, err := h.baseRT.RoundTrip(req)
	return resp, err, true
}
