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

package subroutine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/pm-openfga-operator/internal/subroutine"
	"go.platform-mesh.io/pm-openfga-operator/internal/subroutine/mocks"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

const (
	terminatingAccountClusterID = "account-cluster-id"
	kcpAPIBindingFinalizer      = "apis.kcp.io/apibinding-finalizer"
)

func terminatingAccountLogicalCluster() *kcpcorev1alpha1.LogicalCluster {
	return &kcpcorev1alpha1.LogicalCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
			Annotations: map[string]string{
				"kcp.io/cluster": terminatingAccountClusterID,
				"kcp.io/path":    accountLCPath,
			},
		},
	}
}

func deletingAPIBinding(name string, finalizers ...string) *kcpapisv1alpha2.APIBinding {
	return &kcpapisv1alpha2.APIBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Finalizers:        finalizers,
			DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
		},
	}
}

func newAPIBindingFakeClient(t *testing.T, funcs interceptor.Funcs, objs ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	utilruntime.Must(kcpapisv1alpha2.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithInterceptorFuncs(funcs).
		Build()
}

func TestAPIBindingFinalizerReleaseSubroutine_GetName(t *testing.T) {
	sub := subroutine.NewAPIBindingFinalizerReleaseSubroutine(nil)
	assert.Equal(t, "APIBindingFinalizerReleaseSubroutine", sub.GetName())
}

func TestAPIBindingFinalizerReleaseSubroutine_Terminate(t *testing.T) {
	tests := []struct {
		name     string
		bindings []ctrlruntimeclient.Object
		// remaining maps each APIBinding expected to still exist to its finalizers.
		remaining map[string][]string
		// gone lists APIBindings expected to be deleted once their last finalizer is removed.
		gone []string
	}{
		{
			name:      "no APIBindings",
			remaining: map[string][]string{},
		},
		{
			name: "releases the finalizer and keeps the finalizers of others",
			bindings: []ctrlruntimeclient.Object{
				deletingAPIBinding("dnszones", subroutine.APIBindingFinalizer, kcpAPIBindingFinalizer),
			},
			remaining: map[string][]string{
				"dnszones": {kcpAPIBindingFinalizer},
			},
		},
		{
			name: "releases the last finalizer, so the APIBinding goes",
			bindings: []ctrlruntimeclient.Object{
				deletingAPIBinding("dnszones", subroutine.APIBindingFinalizer),
			},
			gone: []string{"dnszones"},
		},
		{
			name: "releases the finalizer of every APIBinding that carries it",
			bindings: []ctrlruntimeclient.Object{
				deletingAPIBinding("dnszones", subroutine.APIBindingFinalizer, kcpAPIBindingFinalizer),
				deletingAPIBinding("vpcs", subroutine.APIBindingFinalizer, kcpAPIBindingFinalizer),
			},
			remaining: map[string][]string{
				"dnszones": {kcpAPIBindingFinalizer},
				"vpcs":     {kcpAPIBindingFinalizer},
			},
		},
		{
			name: "leaves APIBindings without the finalizer alone",
			bindings: []ctrlruntimeclient.Object{
				deletingAPIBinding("core.platform-mesh.io", kcpAPIBindingFinalizer),
			},
			remaining: map[string][]string{
				"core.platform-mesh.io": {kcpAPIBindingFinalizer},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := newAPIBindingFakeClient(t, interceptor.Funcs{}, tt.bindings...)
			kcpHelper := mocks.NewMockKCPClientGetter(t)
			kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, terminatingAccountClusterID).Return(cl, nil).Once()

			sub := subroutine.NewAPIBindingFinalizerReleaseSubroutine(kcpHelper)
			_, err := sub.Terminate(context.Background(), terminatingAccountLogicalCluster())
			require.NoError(t, err)

			for name, finalizers := range tt.remaining {
				var binding kcpapisv1alpha2.APIBinding
				require.NoError(t, cl.Get(context.Background(), ctrlruntimeclient.ObjectKey{Name: name}, &binding))
				assert.Equal(t, finalizers, binding.Finalizers)
			}
			for _, name := range tt.gone {
				var binding kcpapisv1alpha2.APIBinding
				err := cl.Get(context.Background(), ctrlruntimeclient.ObjectKey{Name: name}, &binding)
				assert.True(t, apierrors.IsNotFound(err), "expected APIBinding %s to be gone, got %v", name, err)
			}
		})
	}
}

func TestAPIBindingFinalizerReleaseSubroutine_TerminateErrors(t *testing.T) {
	errBoom := errors.New("boom")

	tests := []struct {
		name         string
		lc           *kcpcorev1alpha1.LogicalCluster
		clientErr    error
		funcs        interceptor.Funcs
		expectClient bool
	}{
		{
			name: "LogicalCluster without a cluster name",
			lc:   &kcpcorev1alpha1.LogicalCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}},
		},
		{
			name:         "no client for the workspace",
			lc:           terminatingAccountLogicalCluster(),
			clientErr:    errBoom,
			expectClient: true,
		},
		{
			name: "listing APIBindings fails",
			lc:   terminatingAccountLogicalCluster(),
			funcs: interceptor.Funcs{
				List: func(_ context.Context, _ ctrlruntimeclient.WithWatch, _ ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) error {
					return errBoom
				},
			},
			expectClient: true,
		},
		{
			name: "patching an APIBinding fails",
			lc:   terminatingAccountLogicalCluster(),
			funcs: interceptor.Funcs{
				Patch: func(_ context.Context, _ ctrlruntimeclient.WithWatch, _ ctrlruntimeclient.Object, _ ctrlruntimeclient.Patch, _ ...ctrlruntimeclient.PatchOption) error {
					return errBoom
				},
			},
			expectClient: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kcpHelper := mocks.NewMockKCPClientGetter(t)
			if tt.expectClient {
				var cl ctrlruntimeclient.Client
				if tt.clientErr == nil {
					cl = newAPIBindingFakeClient(t, tt.funcs,
						deletingAPIBinding("dnszones", subroutine.APIBindingFinalizer, kcpAPIBindingFinalizer))
				}
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, terminatingAccountClusterID).Return(cl, tt.clientErr).Once()
			}

			sub := subroutine.NewAPIBindingFinalizerReleaseSubroutine(kcpHelper)
			_, err := sub.Terminate(context.Background(), tt.lc)
			assert.Error(t, err)
		})
	}
}
