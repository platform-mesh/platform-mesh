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

	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/nonresourceattributes"

	v1 "k8s.io/api/authorization/v1"
)

func FuzzNonResourceAttributes(f *testing.F) {
	f.Add("/healthz", "/healthz,/readyz,/api")
	f.Add("/api/v1/namespaces", "/api")
	f.Add("/metrics", "/healthz,/readyz")
	f.Add("", "/healthz")
	f.Add("/", "")
	f.Add("/healthz/ready", "/healthz")

	f.Fuzz(func(t *testing.T, path, prefixesCSV string) {
		var prefixes []string
		if prefixesCSV != "" {
			prefixes = splitCSV(prefixesCSV)
		}

		handler := nonresourceattributes.New(prefixes...)
		req := authorization.Request{
			SubjectAccessReview: v1.SubjectAccessReview{
				Spec: v1.SubjectAccessReviewSpec{
					NonResourceAttributes: &v1.NonResourceAttributes{
						Path: path,
					},
				},
			},
		}

		// Must not panic
		handler.Handle(t.Context(), req)
	})
}

func splitCSV(s string) []string {
	var result []string
	start := 0
	for i := range len(s) {
		if s[i] == ',' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}
