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

package kcp

import (
	"errors"
	"testing"

	"go.platform-mesh.io/golang-commons/logger/testlogger"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestOrgAccessValidatorValidateTokenForOrg(t *testing.T) {
	tests := []struct {
		name          string
		authHeader    string
		authenticated bool
		apiErr        error
		valid         bool
		wantErr       bool
	}{
		{
			name:          "authenticated token",
			authHeader:    "Bearer mytoken",
			authenticated: true,
			valid:         true,
		},
		{
			name:          "invalid JWT",
			authHeader:    "Bearer mytoken",
			authenticated: false,
			valid:         false,
		},
		{
			name:       "TokenReview API error",
			authHeader: "Bearer mytoken",
			apiErr:     errors.New("connection refused"),
			wantErr:    true,
		},
		{
			name:       "malformed authorization header",
			authHeader: "notbearer",
			wantErr:    true,
		},
		{
			name:       "empty token in header",
			authHeader: "Bearer ",
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cs := fake.NewSimpleClientset()
			if tc.apiErr != nil {
				cs.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, tc.apiErr
				})
			} else {
				cs.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
					return true, &authenticationv1.TokenReview{
						ObjectMeta: metav1.ObjectMeta{},
						Status:     authenticationv1.TokenReviewStatus{Authenticated: tc.authenticated},
					}, nil
				})
			}

			log := testlogger.New().HideLogOutput().Logger
			validator := &OrgAccessValidator{clientset: cs, log: log}

			valid, err := validator.ValidateTokenForOrg(t.Context(), tc.authHeader, "acme")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if valid != tc.valid {
				t.Fatalf("expected valid=%t, got %t", tc.valid, valid)
			}
		})
	}
}
