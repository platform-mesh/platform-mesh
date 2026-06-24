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

//nolint:goconst
package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// newRBACScheme builds a scheme that includes authorization types.
func newRBACScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	return s
}

// allowedSARClient returns a fake client whose Create interceptor marks every
// SelfSubjectAccessReview as Allowed=true.
func allowedSARClient(t *testing.T) ctrlruntimeclient.Client {
	t.Helper()
	scheme := newRBACScheme(t)
	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
				if sar, ok := obj.(*authorizationv1.SelfSubjectAccessReview); ok {
					sar.Status.Allowed = true
					return nil
				}
				return base.Create(ctx, obj, opts...)
			},
		}).
		Build()
}

// deniedSARClient returns a fake client whose Create interceptor marks every
// SelfSubjectAccessReview as Allowed=false (default, but explicit here).
func deniedSARClient(t *testing.T) ctrlruntimeclient.Client {
	t.Helper()
	scheme := newRBACScheme(t)
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(_ context.Context, _ ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.CreateOption) error {
				if sar, ok := obj.(*authorizationv1.SelfSubjectAccessReview); ok {
					sar.Status.Allowed = false
					return nil
				}
				return nil
			},
		}).
		Build()
}

// errorSARClient returns a fake client whose Create always returns an error for SAR objects.
func errorSARClient(t *testing.T, createErr error) ctrlruntimeclient.Client {
	t.Helper()
	scheme := newRBACScheme(t)
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(_ context.Context, _ ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.CreateOption) error {
				if _, ok := obj.(*authorizationv1.SelfSubjectAccessReview); ok {
					return createErr
				}
				return nil
			},
		}).
		Build()
}

// ---------------------------------------------------------------------------
// CheckTargetPermissions — all verbs allowed
// ---------------------------------------------------------------------------

func TestCheckTargetPermissions_AllVerbs_AllAllowed_ReturnsNil(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	c := allowedSARClient(t)

	err := CheckTargetPermissions(context.Background(), c, gvr)
	require.NoError(t, err, "all verbs allowed should return nil")
}

// ---------------------------------------------------------------------------
// CheckTargetPermissions — permission denied
// ---------------------------------------------------------------------------

func TestCheckTargetPermissions_DeniedVerb_ReturnsError(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	c := deniedSARClient(t)

	err := CheckTargetPermissions(context.Background(), c, gvr)
	require.Error(t, err, "denied verb should return an error")
	assert.Contains(t, err.Error(), "missing permission")
}

func TestCheckTargetPermissions_DeniedVerb_ErrorContainsGVR(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	c := deniedSARClient(t)

	err := CheckTargetPermissions(context.Background(), c, gvr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), gvr.String())
}

// ---------------------------------------------------------------------------
// CheckTargetPermissions — create error
// ---------------------------------------------------------------------------

func TestCheckTargetPermissions_CreateError_WrapsError(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	sentinel := errors.New("api server unavailable")
	c := errorSARClient(t, sentinel)

	err := CheckTargetPermissions(context.Background(), c, gvr)
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

func TestCheckTargetPermissions_CreateError_MessageMentionsVerb(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	c := errorSARClient(t, errors.New("forbidden"))

	err := CheckTargetPermissions(context.Background(), c, gvr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating SSAR for verb")
}
