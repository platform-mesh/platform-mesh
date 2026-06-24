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

package authorization_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization"

	v1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

func TestServeHTTP(t *testing.T) {
	testCases := []struct {
		name               string
		handler            authorization.Handler
		req                func() *http.Request
		responseAssertions func(*testing.T, *http.Response)
	}{
		{
			name: "should fail for nil body",
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/authorize", nil)
			},
			responseAssertions: func(t *testing.T, res *http.Response) {
				var sar v1.SubjectAccessReview
				err := json.NewDecoder(res.Body).Decode(&sar)
				assert.NoError(t, err)

				assert.False(t, sar.Status.Allowed)
				assert.Equal(t, "request body is empty", sar.Status.Reason)
			},
		},
		{
			name: "should fail for no body",
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/authorize", http.NoBody)
			},
			responseAssertions: func(t *testing.T, res *http.Response) {
				var sar v1.SubjectAccessReview
				err := json.NewDecoder(res.Body).Decode(&sar)
				assert.NoError(t, err)

				assert.False(t, sar.Status.Allowed)
				assert.Equal(t, "request body is empty", sar.Status.Reason)
			},
		},
		{
			name: "should fail for wrong content-type",
			req: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/authorize", &bytes.Buffer{})
				req.Header.Set("Content-Type", "wrong")
				return req
			},
			responseAssertions: func(t *testing.T, res *http.Response) {
				var sar v1.SubjectAccessReview
				err := json.NewDecoder(res.Body).Decode(&sar)
				assert.NoError(t, err)

				assert.False(t, sar.Status.Allowed)
				assert.Equal(t, "contentType=wrong, expected application/json", sar.Status.Reason)
			},
		},
		{
			name: "should fail for wrong body content",
			req: func() *http.Request {
				var buffer bytes.Buffer
				buffer.WriteString("{")

				req := httptest.NewRequest(http.MethodPost, "/authorize", &buffer)
				req.Header.Set("Content-Type", "application/json")
				return req
			},
			responseAssertions: func(t *testing.T, res *http.Response) {
				var sar v1.SubjectAccessReview
				err := json.NewDecoder(res.Body).Decode(&sar)
				assert.NoError(t, err)

				assert.False(t, sar.Status.Allowed)
				assert.Equal(t, "couldn't get version/kind; json parse error: unexpected end of JSON input", sar.Status.Reason)
			},
		},
		{
			name: "should return the response from the handler with the UID",
			req: func() *http.Request {
				var buffer bytes.Buffer
				sar := v1.SubjectAccessReview{
					ObjectMeta: metav1.ObjectMeta{
						UID: "1234",
					},
				}
				err := json.NewEncoder(&buffer).Encode(sar)
				assert.NoError(t, err)

				req := httptest.NewRequest(http.MethodPost, "/authorize", &buffer)
				req.Header.Set("Content-Type", "application/json")
				return req
			},
			handler: authorization.HandlerFunc(func(ctx context.Context, r authorization.Request) authorization.Response {
				return authorization.Allowed()
			}),
			responseAssertions: func(t *testing.T, res *http.Response) {
				var sar v1.SubjectAccessReview
				err := json.NewDecoder(res.Body).Decode(&sar)
				assert.NoError(t, err)

				assert.True(t, sar.Status.Allowed)
				assert.Equal(t, types.UID("1234"), sar.UID)
			},
		},
		{
			name: "should set Retry-After header and 503 when handler returns Retry",
			req: func() *http.Request {
				var buffer bytes.Buffer
				sar := v1.SubjectAccessReview{
					ObjectMeta: metav1.ObjectMeta{
						UID: "1234",
					},
				}
				err := json.NewEncoder(&buffer).Encode(sar)
				assert.NoError(t, err)

				req := httptest.NewRequest(http.MethodPost, "/authorize", &buffer)
				req.Header.Set("Content-Type", "application/json")
				return req
			},
			handler: authorization.HandlerFunc(func(ctx context.Context, r authorization.Request) authorization.Response {
				return authorization.Retry(5 * time.Second)
			}),
			responseAssertions: func(t *testing.T, res *http.Response) {
				assert.Equal(t, 503, res.StatusCode)
				assert.Equal(t, "5", res.Header.Get("Retry-After"))

				var sar v1.SubjectAccessReview
				err := json.NewDecoder(res.Body).Decode(&sar)
				assert.Error(t, err, "response body should not contain a SubjectAccessReview when Retry is returned")
			},
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			wh := authorization.New(klog.NewKlogr(), test.handler)

			res := httptest.NewRecorder()

			wh.ServeHTTP(res, test.req())

			handledResponse := res.Result()
			if test.responseAssertions != nil {
				test.responseAssertions(t, handledResponse)
			}
		})
	}
}
