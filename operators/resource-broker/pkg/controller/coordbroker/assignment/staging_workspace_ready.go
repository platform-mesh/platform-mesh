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

package assignment

import (
	"context"
	"fmt"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/resource-broker/pkg/names"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// AssignmentFinalizer is placed on Assignment objects to garbage
	// collect unreferenced StagingWorkspaces.
	AssignmentFinalizer = "broker.platform-mesh.io/assignment"

	// stagingWorkspaceNamePrefix prefixes the hashed StagingWorkspace names.
	stagingWorkspaceNamePrefix = "staging-"
)

// stagingWorkspaceReadySubroutine ensures the StagingWorkspace serving an Assignment exists and is ready.
type stagingWorkspaceReadySubroutine struct {
	opts Options
}

var (
	_ subroutines.Processor = &stagingWorkspaceReadySubroutine{}
	_ subroutines.Finalizer = &stagingWorkspaceReadySubroutine{}
)

func (s *stagingWorkspaceReadySubroutine) GetName() string {
	return pmcoordbrokerv1alpha1.AssignmentConditionStagingWorkspaceReady
}

func (s *stagingWorkspaceReadySubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{AssignmentFinalizer}
}

func (s *stagingWorkspaceReadySubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	assignment, ok := obj.(*pmcoordbrokerv1alpha1.Assignment)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Assignment, got %T", obj)
	}

	cl, err := subroutines.ClientFromContext(ctx)
	if err != nil {
		return subroutines.Result{}, err
	}

	if assignment.Status.APIExportName == "" {
		exportName, err := s.resolveAPIExportName(ctx, assignment)
		if err != nil {
			return subroutines.Result{}, err
		}
		assignment.Status.APIExportName = exportName
	}

	wsName := stagingWorkspaceName(assignment.Spec.ConsumerCluster, assignment.Spec.ProviderCluster, assignment.Status.APIExportName)
	assignment.Status.StagingWorkspace = wsName

	sw := &pmcoordbrokerv1alpha1.StagingWorkspace{}
	err = cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: wsName}, sw)
	switch {
	case apierrors.IsNotFound(err):
		sw = &pmcoordbrokerv1alpha1.StagingWorkspace{
			ObjectMeta: metav1.ObjectMeta{
				Name: wsName,
			},
			Spec: pmcoordbrokerv1alpha1.StagingWorkspaceSpec{
				ConsumerCluster: assignment.Spec.ConsumerCluster,
				ProviderCluster: assignment.Spec.ProviderCluster,
				APIExportName:   assignment.Status.APIExportName,
			},
		}
		if err := cl.Create(ctx, sw); err != nil {
			return subroutines.Result{}, fmt.Errorf("creating StagingWorkspace %q: %w", wsName, err)
		}
		assignment.Status.Phase = pmcoordbrokerv1alpha1.AssignmentPhasePending
		return subroutines.Pending(s.opts.RequeueInterval, "created staging workspace"), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting StagingWorkspace %q: %w", wsName, err)
	}

	if !sw.DeletionTimestamp.IsZero() {
		assignment.Status.Phase = pmcoordbrokerv1alpha1.AssignmentPhasePending
		return subroutines.Pending(s.opts.RequeueInterval, "staging workspace is terminating"), nil
	}

	if sw.Status.Phase != pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady {
		assignment.Status.Phase = pmcoordbrokerv1alpha1.AssignmentPhasePending
		return subroutines.Pending(s.opts.RequeueInterval, "waiting for staging workspace to become ready"), nil
	}

	assignment.Status.Phase = pmcoordbrokerv1alpha1.AssignmentPhaseBound
	return subroutines.OK(), nil
}

// Finalize deletes the StagingWorkspace if no other Assignment references it.
func (s *stagingWorkspaceReadySubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	assignment, ok := obj.(*pmcoordbrokerv1alpha1.Assignment)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Assignment, got %T", obj)
	}

	assignment.Status.Phase = pmcoordbrokerv1alpha1.AssignmentPhaseTerminating

	wsName := assignment.Status.StagingWorkspace
	if wsName == "" {
		return subroutines.OK(), nil
	}

	cl, err := subroutines.ClientFromContext(ctx)
	if err != nil {
		return subroutines.Result{}, err
	}

	assignments := &pmcoordbrokerv1alpha1.AssignmentList{}
	if err := cl.List(ctx, assignments); err != nil {
		return subroutines.Result{}, fmt.Errorf("listing Assignments: %w", err)
	}
	for _, other := range assignments.Items {
		if other.Name == assignment.Name || !other.DeletionTimestamp.IsZero() {
			continue
		}
		if other.Status.StagingWorkspace == wsName {
			return subroutines.OK(), nil
		}
	}

	sw := &pmcoordbrokerv1alpha1.StagingWorkspace{
		ObjectMeta: metav1.ObjectMeta{
			Name: wsName,
		},
	}
	if err := cl.Delete(ctx, sw); ctrlruntimeclient.IgnoreNotFound(err) != nil {
		return subroutines.Result{}, fmt.Errorf("deleting unreferenced StagingWorkspace %q: %w", wsName, err)
	}

	return subroutines.OK(), nil
}

// resolveAPIExportName reads the APIExport name from the AcceptAPI in the provider workspace.
func (s *stagingWorkspaceReadySubroutine) resolveAPIExportName(ctx context.Context, assignment *pmcoordbrokerv1alpha1.Assignment) (string, error) {
	providerClient, err := s.opts.WorkspaceClientFunc(assignment.Spec.ProviderCluster)
	if err != nil {
		return "", fmt.Errorf("building client for provider cluster %q: %w", assignment.Spec.ProviderCluster, err)
	}

	acceptAPI := &pmbrokerv1alpha1.AcceptAPI{}
	if err := providerClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: assignment.Spec.AcceptAPIName}, acceptAPI); err != nil {
		return "", fmt.Errorf("getting AcceptAPI %q in provider cluster %q: %w", assignment.Spec.AcceptAPIName, assignment.Spec.ProviderCluster, err)
	}

	return acceptAPI.Spec.APIExportName, nil
}

func stagingWorkspaceName(consumerCluster, providerCluster, apiExportName string) string {
	return stagingWorkspaceNamePrefix + names.Hash(consumerCluster, providerCluster, apiExportName)
}
