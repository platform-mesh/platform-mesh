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

package nonresourceattributes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/nonresourceattributes"

	v1 "k8s.io/api/authorization/v1"
)

func TestHandler(t *testing.T) {
	testCases := []struct {
		name                string
		allowedPathPrefixes []string
		req                 authorization.Request
		res                 authorization.Response
	}{
		{
			name: "should skip processing if no nonResourceAttributes are present",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						NonResourceAttributes: nil,
					},
				},
			},
			res: authorization.NoOpinion(),
		},
		{
			name: "should allow if path matches allowed prefix",
			allowedPathPrefixes: []string{
				"/healthz",
				"/readyz",
			},
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						NonResourceAttributes: &v1.NonResourceAttributes{
							Path: "/healthz",
						},
					},
				},
			},
			res: authorization.Allowed(),
		},
		{
			name: "should allow if path matches allowed prefix",
			allowedPathPrefixes: []string{
				"/api",
			},
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						NonResourceAttributes: &v1.NonResourceAttributes{
							Path: "/api/v1/namespaces",
						},
					},
				},
			},
			res: authorization.Allowed(),
		},
		{
			name: "should Abort if path does not match allowed prefix",
			allowedPathPrefixes: []string{
				"/api",
			},
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						NonResourceAttributes: &v1.NonResourceAttributes{
							Path: "/healthz",
						},
					},
				},
			},
			res: authorization.Aborted(),
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			h := nonresourceattributes.New(test.allowedPathPrefixes...)

			ctx := t.Context()

			res := h.Handle(ctx, test.req)

			assert.Equal(t, test.res, res)
		})
	}
}
