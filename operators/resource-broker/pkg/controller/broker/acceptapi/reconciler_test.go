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

package acceptapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, pmbrokerv1alpha1.AddToScheme(scheme))
	require.NoError(t, kcptenancyv1alpha1.AddToScheme(scheme))
	require.NoError(t, kcpapisv1alpha2.AddToScheme(scheme))
	return scheme
}

func TestNewReconcilerValidation(t *testing.T) {
	clientFunc := func(string) (ctrlruntimeclient.Client, error) {
		return fake.NewClientBuilder().Build(), nil
	}

	tests := []struct {
		name    string
		opts    Options
		wantErr string
	}{
		{
			name:    "missing tree root",
			opts:    Options{WorkspaceClientFunc: clientFunc},
			wantErr: "VerificationTreeRoot is required",
		},
		{
			name: "missing workspace client func",
			opts: Options{
				VerificationTreeRoot: "root:platform",
			},
			wantErr: "WorkspaceClientFunc is required",
		},
		{
			name: "tree root and workspace client func",
			opts: Options{
				VerificationTreeRoot: "root:platform",
				WorkspaceClientFunc:  clientFunc,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewReconciler(nil, tt.opts)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, r)
		})
	}
}
