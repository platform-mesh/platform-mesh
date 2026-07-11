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

package brokeredresource

import (
	"context"
	"fmt"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/resource-broker/pkg/names"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

const (
	// AssignmentFinalizer is placed on consumer objects to clean up
	// their Assignment on deletion.
	AssignmentFinalizer = "broker.platform-mesh.io/assignment-ref"

	// assignmentNamePrefix prefixes the hashed Assignment names.
	assignmentNamePrefix = "assignment-"
)

// assignmentSubroutine ensures the Assignment for a consumer object exists and is bound.
type assignmentSubroutine struct {
	opts Options
}

var (
	_ subroutines.Processor = &assignmentSubroutine{}
	_ subroutines.Finalizer = &assignmentSubroutine{}
)

func (s *assignmentSubroutine) GetName() string {
	return "Assignment"
}

func (s *assignmentSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{AssignmentFinalizer}
}

func (s *assignmentSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Unstructured, got %T", obj)
	}

	cluster, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("no cluster name in context")
	}
	consumerCluster := cluster.String()

	name := s.assignmentName(consumerCluster, u)

	assignment := &pmcoordbrokerv1alpha1.Assignment{}
	err := s.opts.CoordinationClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, assignment)
	switch {
	case apierrors.IsNotFound(err):
		return s.createAssignment(ctx, name, consumerCluster, u)
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting Assignment %q: %w", name, err)
	}

	if !assignment.DeletionTimestamp.IsZero() {
		return subroutines.Pending(s.opts.RequeueInterval, "assignment is terminating"), nil
	}

	if assignment.Status.Phase != pmcoordbrokerv1alpha1.AssignmentPhaseBound {
		return subroutines.Pending(s.opts.RequeueInterval, "waiting for assignment to be bound"), nil
	}

	return subroutines.OK(), nil
}

// Finalize deletes the Assignment and waits for it to be gone.
func (s *assignmentSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Unstructured, got %T", obj)
	}

	cluster, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("no cluster name in context")
	}

	name := s.assignmentName(cluster.String(), u)

	assignment := &pmcoordbrokerv1alpha1.Assignment{}
	err := s.opts.CoordinationClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, assignment)
	switch {
	case apierrors.IsNotFound(err):
		return subroutines.OK(), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting Assignment %q: %w", name, err)
	}

	if assignment.DeletionTimestamp.IsZero() {
		if err := s.opts.CoordinationClient.Delete(ctx, assignment); ctrlruntimeclient.IgnoreNotFound(err) != nil {
			return subroutines.Result{}, fmt.Errorf("deleting Assignment %q: %w", name, err)
		}
	}

	return subroutines.Pending(s.opts.RequeueInterval, "waiting for assignment to be deleted"), nil
}

// createAssignment picks a provider among matching AcceptAPIs and creates the Assignment.
func (s *assignmentSubroutine) createAssignment(ctx context.Context, name, consumerCluster string, u *unstructured.Unstructured) (subroutines.Result, error) {
	refs, err := s.opts.ListAcceptAPIs(ctx)
	if err != nil {
		return subroutines.Result{}, fmt.Errorf("listing AcceptAPIs: %w", err)
	}

	var matching []AcceptAPIRef
	for _, ref := range refs {
		if ok, _ := ref.AcceptAPI.AppliesTo(s.opts.GVR, u); ok {
			matching = append(matching, ref)
		}
	}
	if len(matching) == 0 {
		return subroutines.Pending(s.opts.RequeueInterval, "no matching AcceptAPI"), nil
	}

	pick := s.opts.PickAcceptAPI(matching)

	assignment := &pmcoordbrokerv1alpha1.Assignment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: pmcoordbrokerv1alpha1.AssignmentSpec{
			ConsumerCluster: consumerCluster,
			GVR:             s.opts.GVR,
			Namespace:       u.GetNamespace(),
			Name:            u.GetName(),
			ProviderCluster: pick.Cluster,
			AcceptAPIName:   pick.AcceptAPI.Name,
		},
	}
	if err := s.opts.CoordinationClient.Create(ctx, assignment); err != nil {
		return subroutines.Result{}, fmt.Errorf("creating Assignment %q: %w", name, err)
	}

	return subroutines.Pending(s.opts.RequeueInterval, "created assignment"), nil
}

func (s *assignmentSubroutine) assignmentName(consumerCluster string, u *unstructured.Unstructured) string {
	return assignmentName(consumerCluster, s.opts.GVR, u.GetNamespace(), u.GetName())
}

func assignmentName(consumerCluster string, gvr metav1.GroupVersionResource, namespace, name string) string {
	return assignmentNamePrefix + names.Hash(consumerCluster, gvr.Group, gvr.Version, gvr.Resource, namespace, name)
}
