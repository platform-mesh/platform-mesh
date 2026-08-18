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

package restore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentVirtualWorkspacePathResolved(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/services/contentconfigurations/clusters/root:orgs:sap/apis/ui.platform-mesh.io/v1alpha1/contentconfigurations", r.URL.Path)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden: regular resource RBAC"}`))
	}))
	defer server.Close()

	resolved, err := contentVirtualWorkspacePathResolved(
		context.Background(), server.Client(), server.URL,
		"/services/contentconfigurations/clusters/root:orgs:sap/apis/ui.platform-mesh.io/v1alpha1/contentconfigurations",
	)
	require.NoError(t, err)
	require.True(t, resolved)
}

func TestContentVirtualWorkspacePathResolvedDetectsUnresolvedRoute(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Path not resolved to a valid virtual workspace"}`))
	}))
	defer server.Close()

	resolved, err := contentVirtualWorkspacePathResolved(
		context.Background(), server.Client(), server.URL,
		"/services/contentconfigurations/clusters/root:orgs:sap/apis/ui.platform-mesh.io/v1alpha1/contentconfigurations",
	)
	require.NoError(t, err)
	require.False(t, resolved)
}
