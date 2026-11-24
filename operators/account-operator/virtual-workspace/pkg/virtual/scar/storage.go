/*
Copyright 2026 The kcp Authors.

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

// Package scar implements the SelfClusterAccessReview API of the
// Access Virtual Workspace.
package scar

import (
	"context"
	"fmt"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/accessprovider"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/graph"
	accessv1alpha1 "go.platform-mesh.io/account-operator/virtual-workspace/sdk/apis/access/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/klog/v2"
)

type REST struct {
	provider accessprovider.AccessProvider
}

var (
	_ rest.Storage              = &REST{}
	_ rest.Creater              = &REST{} //nolint:misspell // the name of the interface
	_ rest.Scoper               = &REST{}
	_ rest.SingularNameProvider = &REST{}
)

// NewREST returns the REST storage answering from the given provider.
func NewREST(provider accessprovider.AccessProvider) *REST {
	return &REST{provider: provider}
}

// New returns an empty SelfClusterAccessReview.
func (r *REST) New() runtime.Object {
	return &accessv1alpha1.SelfClusterAccessReview{}
}

// Destroy is a no-op; the storage holds no resources that need cleanup.
func (r *REST) Destroy() {}

// NamespaceScoped reports that the resource is cluster-scoped.
func (r *REST) NamespaceScoped() bool {
	return false
}

// GetSingularName returns the singular resource name.
func (r *REST) GetSingularName() string {
	return "selfclusteraccessreview"
}

// Create answers the self-review, it resolves the caller from the
// request context and fills Status.Clusters from the provider.
func (r *REST) Create(ctx context.Context, obj runtime.Object, _ rest.ValidateObjectFunc, _ *metav1.CreateOptions) (runtime.Object, error) {
	review, ok := obj.(*accessv1alpha1.SelfClusterAccessReview)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("not a SelfClusterAccessReview: %T", obj))
	}

	if !r.provider.Ready() {
		return nil, apierrors.NewServiceUnavailable("access provider is not ready; try again shortly")
	}

	user, ok := genericapirequest.UserFrom(ctx)
	if !ok {
		return nil, apierrors.NewUnauthorized("no user present in request context")
	}

	scope := ClusterFrom(ctx)

	clusters, err := r.provider.ClustersFor(ctx, accessprovider.IdentityOf(user), scope)
	if err != nil {
		return nil, err
	}

	klog.FromContext(ctx).V(4).Info("answered SelfClusterAccessReview",
		"username", user.GetName(),
		"groups", user.GetGroups(),
		"scope", scope,
		"clusters", len(clusters),
	)

	out := review.DeepCopy()
	out.Status = accessv1alpha1.SelfClusterAccessReviewStatus{
		Clusters: toAccessEndpointSlices(clusters),
	}

	return out, nil
}

func toAccessEndpointSlices(in []graph.AccessEndpointSlice) []accessv1alpha1.AccessEndpointSlice {
	if in == nil {
		return nil
	}

	out := make([]accessv1alpha1.AccessEndpointSlice, len(in))
	for i, s := range in {
		out[i] = accessv1alpha1.AccessEndpointSlice{
			ClusterName: s.ClusterName,
			Endpoint:    s.Endpoint,
		}
		if a := s.Account; a != nil {
			out[i].Account = &accessv1alpha1.Account{Name: a.Name, Path: a.Path, DisplayName: a.DisplayName, Type: a.Type, Ready: a.Ready}
		}
	}

	return out
}
