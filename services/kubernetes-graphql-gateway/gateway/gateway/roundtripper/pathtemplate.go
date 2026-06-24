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
	"strings"

	utilscontext "go.platform-mesh.io/kubernetes-graphql-gateway/gateway/utils/context"
)

type PathTemplateHandler struct {
	inner    http.RoundTripper
	prefix   string
	basePath string
}

func NewPathTemplateHandler(inner http.RoundTripper, prefix, basePath string) *PathTemplateHandler {
	return &PathTemplateHandler{inner: inner, prefix: prefix, basePath: basePath}
}

func (h *PathTemplateHandler) RoundTrip(req *http.Request) (*http.Response, error) {
	if h.prefix == "" {
		return h.inner.RoundTrip(req)
	}

	target, ok := utilscontext.GetClusterTargetFromCtx(req.Context())
	if !ok || target == "" {
		return h.inner.RoundTrip(req)
	}

	resolved := strings.ReplaceAll(h.prefix, "{clusterTarget}", target)
	newReq := req.Clone(req.Context())
	newReq.URL.Path = resolved + strings.TrimPrefix(req.URL.Path, h.basePath)

	return h.inner.RoundTrip(newReq)
}
