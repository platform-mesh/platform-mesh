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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/accessprovider"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/graph"
	accessv1alpha1 "go.platform-mesh.io/account-operator/virtual-workspace/sdk/apis/access/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/klog/v2"
)

// SelfName is the name of the caller's review in a list or a watch.
const SelfName = "self"

// DefaultWatchInterval is how often an open watch asks the provider again.
const DefaultWatchInterval = 10 * time.Second

type REST struct {
	rest.TableConvertor

	provider accessprovider.AccessProvider
	// interval is how often an open watch asks the provider again.
	interval time.Duration
}

var (
	_ rest.Storage              = &REST{}
	_ rest.Creater              = &REST{} //nolint:misspell // the name of the interface
	_ rest.Lister               = &REST{}
	_ rest.Watcher              = &REST{}
	_ rest.Scoper               = &REST{}
	_ rest.SingularNameProvider = &REST{}
)

// NewREST returns the REST storage answering from the given provider.
func NewREST(provider accessprovider.AccessProvider) *REST {
	return &REST{
		TableConvertor: rest.NewDefaultTableConvertor(accessv1alpha1.Resource("selfclusteraccessreviews")),
		provider:       provider,
		interval:       DefaultWatchInterval,
	}
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

	out := review.DeepCopy()
	status, err := r.status(ctx)
	if err != nil {
		return nil, err
	}
	out.Status = status

	return out, nil
}

// NewList returns an empty SelfClusterAccessReviewList.
func (r *REST) NewList() runtime.Object {
	return &accessv1alpha1.SelfClusterAccessReviewList{}
}

// List returns one review, the caller's, named SelfName. Its resource
// version is a hash of its status, as nothing is stored that could count
// changes. Selectors are ignored.
func (r *REST) List(ctx context.Context, _ *metainternalversion.ListOptions) (runtime.Object, error) {
	review, err := r.self(ctx)
	if err != nil {
		return nil, err
	}

	return &accessv1alpha1.SelfClusterAccessReviewList{
		ListMeta: metav1.ListMeta{ResourceVersion: review.ResourceVersion},
		Items:    []accessv1alpha1.SelfClusterAccessReview{*review},
	}, nil
}

// Watch reports when the caller's review changes. The providers do not
// report changes, so the watch asks again every interval and sends the
// review as MODIFIED when its status differs.
//
// Without a resource version the watch starts with the review as ADDED; with
// the version of the current review it reports what changes from then on;
// with any other version it fails as expired, and the client lists again.
func (r *REST) Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error) {
	review, err := r.self(ctx)
	if err != nil {
		return nil, err
	}

	var first *watch.Event
	if rv := options.ResourceVersion; rv == "" || rv == "0" {
		first = &watch.Event{Type: watch.Added, Object: review}
	} else if rv != review.ResourceVersion {
		return nil, apierrors.NewResourceExpired("the review changed since resource version " + rv + "; list again")
	}
	sent := review.ResourceVersion

	events := make(chan watch.Event)
	watcher := watch.NewProxyWatcher(events)
	send := func(event watch.Event) bool {
		select {
		case events <- event:
			return true
		case <-ctx.Done():
		case <-watcher.StopChan():
		}
		return false
	}

	go func() {
		defer close(events)

		if first != nil && !send(*first) {
			return
		}

		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			case <-watcher.StopChan():
				return
			}

			review, err := r.self(ctx)
			if err != nil {
				if ctx.Err() == nil {
					// The client watches again from the version it has.
					klog.FromContext(ctx).V(2).Info("ending SelfClusterAccessReview watch", "err", err)
					send(watch.Event{Type: watch.Error, Object: statusOf(err)})
				}
				return
			}
			if review.ResourceVersion == sent {
				continue
			}
			if !send(watch.Event{Type: watch.Modified, Object: review}) {
				return
			}
			sent = review.ResourceVersion
		}
	}()

	return watcher, nil
}

// self returns the caller's review as a list and a watch carry it.
func (r *REST) self(ctx context.Context) (*accessv1alpha1.SelfClusterAccessReview, error) {
	status, err := r.status(ctx)
	if err != nil {
		return nil, err
	}

	// These types cannot fail to encode.
	data, _ := json.Marshal(status)
	sum := sha256.Sum256(data)

	return &accessv1alpha1.SelfClusterAccessReview{
		ObjectMeta: metav1.ObjectMeta{Name: SelfName, ResourceVersion: hex.EncodeToString(sum[:8])},
		Status:     status,
	}, nil
}

// status resolves the caller from the request context and asks the provider.
func (r *REST) status(ctx context.Context) (accessv1alpha1.SelfClusterAccessReviewStatus, error) {
	var none accessv1alpha1.SelfClusterAccessReviewStatus

	if !r.provider.Ready() {
		return none, apierrors.NewServiceUnavailable("access provider is not ready; try again shortly")
	}

	user, ok := genericapirequest.UserFrom(ctx)
	if !ok {
		return none, apierrors.NewUnauthorized("no user present in request context")
	}

	scope := ClusterFrom(ctx)

	clusters, err := r.provider.ClustersFor(ctx, accessprovider.IdentityOf(user), scope)
	if err != nil {
		return none, err
	}

	klog.FromContext(ctx).V(4).Info("answered SelfClusterAccessReview",
		"username", user.GetName(),
		"groups", user.GetGroups(),
		"scope", scope,
		"clusters", len(clusters),
	)

	return accessv1alpha1.SelfClusterAccessReviewStatus{Clusters: toAccessEndpointSlices(clusters)}, nil
}

func statusOf(err error) *metav1.Status {
	if status, ok := err.(apierrors.APIStatus); ok { //nolint:errorlint // providers return API status errors as they are
		s := status.Status()
		return &s
	}
	s := apierrors.NewInternalError(err).Status()

	return &s
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
