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

package union

import (
	"context"

	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization"

	"k8s.io/klog/v2"
)

type authorizationUnion struct {
	Handlers []authorization.Handler
}

// Handle implements authorization.Handler.
func (u *authorizationUnion) Handle(ctx context.Context, req authorization.Request) authorization.Response {
	for _, h := range u.Handlers {
		resp := h.Handle(ctx, req)
		// if there is an explicit response from one of the handlers, return it
		if resp.Status.Allowed || resp.Status.Denied || resp.Abort || resp.RetryAfter != 0 {
			return resp
		}
	}

	klog.V(5).Info("Union handler returning implicit NoOpinion")
	return authorization.NoOpinion()
}

var _ authorization.Handler = &authorizationUnion{}

func New(requestHandlers ...authorization.Handler) authorization.Handler {
	if len(requestHandlers) == 1 {
		return requestHandlers[0]
	}

	return &authorizationUnion{
		Handlers: requestHandlers,
	}
}
