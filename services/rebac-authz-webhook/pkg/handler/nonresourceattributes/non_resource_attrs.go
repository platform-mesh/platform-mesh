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

package nonresourceattributes

import (
	"context"
	"strings"

	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization"

	"k8s.io/klog/v2"
)

type nonResourceAttributesAuthorizer struct {
	allowedPathPrefixes []string
}

var _ authorization.Handler = &nonResourceAttributesAuthorizer{}

func New(allowedPathPrefixes ...string) authorization.Handler {
	return &nonResourceAttributesAuthorizer{
		allowedPathPrefixes,
	}
}

func (n *nonResourceAttributesAuthorizer) Handle(ctx context.Context, req authorization.Request) authorization.Response {

	klog.V(5).Info("handling request in NonResourceAttributesAuthorizer")

	if req.Spec.NonResourceAttributes == nil {
		klog.V(5).Info("request does not contain NonResourceAttributes, skipping")
		return authorization.NoOpinion()
	}

	attrs := req.Spec.NonResourceAttributes

	for _, prefix := range n.allowedPathPrefixes {
		if strings.HasPrefix(attrs.Path, prefix) {
			klog.V(5).Infof("request path %q matches allowed prefix %q, allowing", attrs.Path, prefix)
			return authorization.Allowed()
		}
	}

	return authorization.Aborted()
}
