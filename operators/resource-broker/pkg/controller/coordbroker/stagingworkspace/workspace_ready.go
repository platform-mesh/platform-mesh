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

package stagingworkspace

import (
	"context"
	"fmt"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// StagingFinalizer is placed on StagingWorkspace objects to delete their backing kcp workspaces.
const StagingFinalizer = "broker.platform-mesh.io/staging"

// workspaceReadySubroutine ensures the kcp workspace backing a StagingWorkspace exists and is ready.
type workspaceReadySubroutine struct {
	opts Options
}

var (
	_ subroutines.Processor = &workspaceReadySubroutine{}
	_ subroutines.Finalizer = &workspaceReadySubroutine{}
)

func (s *workspaceReadySubroutine) GetName() string {
	return pmcoordbrokerv1alpha1.StagingWorkspaceConditionWorkspaceReady
}

func (s *workspaceReadySubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{StagingFinalizer}
}

func (s *workspaceReadySubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	sw, ok := obj.(*pmcoordbrokerv1alpha1.StagingWorkspace)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected StagingWorkspace, got %T", obj)
	}

	treeClient, err := s.treeRootClient()
	if err != nil {
		return subroutines.Result{}, err
	}

	workspace := &kcptenancyv1alpha1.Workspace{}
	err = treeClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: sw.Name}, workspace)
	switch {
	case apierrors.IsNotFound(err):
		workspace = &kcptenancyv1alpha1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name: sw.Name,
			},
		}
		if err := treeClient.Create(ctx, workspace); err != nil {
			return subroutines.Result{}, fmt.Errorf("creating staging workspace %q: %w", sw.Name, err)
		}
		sw.Status.Phase = pmcoordbrokerv1alpha1.StagingWorkspacePhasePending
		return subroutines.Pending(s.opts.RequeueInterval, "created staging workspace"), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting staging workspace %q: %w", sw.Name, err)
	}

	if !workspace.DeletionTimestamp.IsZero() {
		sw.Status.Phase = pmcoordbrokerv1alpha1.StagingWorkspacePhasePending
		return subroutines.Pending(s.opts.RequeueInterval, "staging workspace is terminating"), nil
	}

	sw.Status.ClusterName = workspace.Spec.Cluster

	if workspace.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
		sw.Status.Phase = pmcoordbrokerv1alpha1.StagingWorkspacePhasePending
		return subroutines.Pending(s.opts.RequeueInterval, "waiting for staging workspace to become ready"), nil
	}

	return subroutines.OK(), nil
}

func (s *workspaceReadySubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	sw, ok := obj.(*pmcoordbrokerv1alpha1.StagingWorkspace)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected StagingWorkspace, got %T", obj)
	}

	sw.Status.Phase = pmcoordbrokerv1alpha1.StagingWorkspacePhaseTerminating

	treeClient, err := s.treeRootClient()
	if err != nil {
		return subroutines.Result{}, err
	}

	workspace := &kcptenancyv1alpha1.Workspace{}
	err = treeClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: sw.Name}, workspace)
	switch {
	case apierrors.IsNotFound(err):
		return subroutines.OK(), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting staging workspace %q: %w", sw.Name, err)
	}

	if workspace.DeletionTimestamp.IsZero() {
		if err := treeClient.Delete(ctx, workspace); ctrlruntimeclient.IgnoreNotFound(err) != nil {
			return subroutines.Result{}, fmt.Errorf("deleting staging workspace %q: %w", sw.Name, err)
		}
	}

	return subroutines.Pending(s.opts.RequeueInterval, "waiting for staging workspace to be deleted"), nil
}

func (s *workspaceReadySubroutine) treeRootClient() (ctrlruntimeclient.Client, error) {
	cl, err := s.opts.WorkspaceClientFunc(s.opts.StagingTreeRoot)
	if err != nil {
		return nil, fmt.Errorf("building client for tree root %q: %w", s.opts.StagingTreeRoot, err)
	}
	return cl, nil
}
