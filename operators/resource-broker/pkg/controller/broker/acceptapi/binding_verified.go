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
	"context"
	"fmt"
	"strings"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	"go.platform-mesh.io/resource-broker/pkg/names"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	// VerificationFinalizer is placed on AcceptAPI object to delete their respective verification workspaces.
	VerificationFinalizer = "broker.platform-mesh.io/verification"

	// workspaceNamePrefix prefixes the names of verification workspaces.
	workspaceNamePrefix = "verify-"

	// refFinalizerPrefix is the prefix used for finalizers on verification workspaces.
	// One APIExport may have multiple AcceptAPIs matching (either due
	// to multiple GVRs being accepted or different filters).
	// To prevent one AcceptAPI being deleted deleting the verification
	// workspace for all of the AcceptAPIs finalizers are placed on the
	// workspace.
	// The workspace is only deleted after the last AcceptAPI finalizer is removed.
	refFinalizerPrefix = "broker.platform-mesh.io/acceptapi-"
)

// bindingVerifiedSubroutine verifies that the APIExport announced by an AcceptAPI is bindable.
type bindingVerifiedSubroutine struct {
	opts Options
}

var (
	_ subroutines.Processor = &bindingVerifiedSubroutine{}
	_ subroutines.Finalizer = &bindingVerifiedSubroutine{}
)

func (s *bindingVerifiedSubroutine) GetName() string {
	return pmbrokerv1alpha1.AcceptAPIConditionBindingVerified
}

func (s *bindingVerifiedSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{VerificationFinalizer}
}

func (s *bindingVerifiedSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	acceptAPI, ok := obj.(*pmbrokerv1alpha1.AcceptAPI)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected AcceptAPI, got %T", obj)
	}

	cluster, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("no cluster name in context")
	}
	providerCluster := cluster.String()

	refFinalizer := referenceFinalizer(providerCluster, acceptAPI)
	wsName := workspaceName(providerCluster, acceptAPI.Spec.APIExportName)

	// The spec changed since the last reconcile: release the reference on
	// the previous verification workspace so it does not leak.
	if old := acceptAPI.Status.VerificationWorkspace; old != "" && old != wsName {
		if err := s.releaseWorkspace(ctx, old, refFinalizer); err != nil {
			return subroutines.Result{}, fmt.Errorf("releasing previous verification workspace %q: %w", old, err)
		}
	}

	result, err := s.ensureWorkspace(ctx, acceptAPI, wsName, refFinalizer)
	if err != nil || !result.IsContinue() || result.Requeue() > 0 {
		return result, err
	}

	return s.ensureBinding(ctx, acceptAPI, wsName, providerCluster)
}

func (s *bindingVerifiedSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	acceptAPI, ok := obj.(*pmbrokerv1alpha1.AcceptAPI)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected AcceptAPI, got %T", obj)
	}

	wsName := acceptAPI.Status.VerificationWorkspace
	if wsName == "" {
		return subroutines.OK(), nil
	}

	cluster, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("no cluster name in context")
	}
	refFinalizer := referenceFinalizer(cluster.String(), acceptAPI)

	if err := s.releaseWorkspace(ctx, wsName, refFinalizer); err != nil {
		return subroutines.Result{}, fmt.Errorf("releasing verification workspace %q: %w", wsName, err)
	}

	acceptAPI.Status.VerificationWorkspace = ""
	return subroutines.OK(), nil
}

func (s *bindingVerifiedSubroutine) treeRootClient() (ctrlruntimeclient.Client, error) {
	cl, err := s.opts.WorkspaceClientFunc(s.opts.VerificationTreeRoot)
	if err != nil {
		return nil, fmt.Errorf("building client for tree root %q: %w", s.opts.VerificationTreeRoot, err)
	}
	return cl, nil
}

// ensureWorkspace ensures the verification workspace exists with finalizer etcpp
func (s *bindingVerifiedSubroutine) ensureWorkspace(ctx context.Context, acceptAPI *pmbrokerv1alpha1.AcceptAPI, wsName, refFinalizer string) (subroutines.Result, error) {
	treeClient, err := s.treeRootClient()
	if err != nil {
		return subroutines.Result{}, err
	}

	workspace := &kcptenancyv1alpha1.Workspace{}
	err = treeClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: wsName}, workspace)
	switch {
	case apierrors.IsNotFound(err):
		workspace = &kcptenancyv1alpha1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:       wsName,
				Finalizers: []string{refFinalizer},
			},
		}
		if err := treeClient.Create(ctx, workspace); err != nil {
			return subroutines.Result{}, fmt.Errorf("creating verification workspace %q: %w", wsName, err)
		}
		acceptAPI.Status.VerificationWorkspace = wsName
		return subroutines.Pending(s.opts.RequeueInterval, "created verification workspace"), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting verification workspace %q: %w", wsName, err)
	}

	if !workspace.DeletionTimestamp.IsZero() {
		// workspace is being deleted, requeue
		return subroutines.Pending(s.opts.RequeueInterval, "verification workspace is terminating"), nil
	}

	if !controllerutil.ContainsFinalizer(workspace, refFinalizer) {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := treeClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: wsName}, workspace); err != nil {
				return err
			}
			if !controllerutil.AddFinalizer(workspace, refFinalizer) {
				return nil
			}
			return treeClient.Update(ctx, workspace)
		})
		if err != nil {
			return subroutines.Result{}, fmt.Errorf("adding reference finalizer to workspace %q: %w", wsName, err)
		}
	}

	acceptAPI.Status.VerificationWorkspace = wsName

	if workspace.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
		return subroutines.Pending(s.opts.RequeueInterval, "waiting for verification workspace to become ready"), nil
	}

	return subroutines.OK(), nil
}

// ensureBinding ensures an APIBinding for the APIExport in the AcceptaPI exists in the vrification workspace and is bound successfully.
func (s *bindingVerifiedSubroutine) ensureBinding(ctx context.Context, acceptAPI *pmbrokerv1alpha1.AcceptAPI, wsName, providerCluster string) (subroutines.Result, error) {
	wsPath := s.opts.VerificationTreeRoot + ":" + wsName
	wsClient, err := s.opts.WorkspaceClientFunc(wsPath)
	if err != nil {
		return subroutines.Result{}, fmt.Errorf("building client for workspace %q: %w", wsPath, err)
	}

	exportName := acceptAPI.Spec.APIExportName
	binding := &kcpapisv1alpha2.APIBinding{}
	err = wsClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: exportName}, binding)
	switch {
	case apierrors.IsNotFound(err):
		binding = &kcpapisv1alpha2.APIBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: exportName,
			},
			Spec: kcpapisv1alpha2.APIBindingSpec{
				Reference: kcpapisv1alpha2.BindingReference{
					Export: &kcpapisv1alpha2.ExportBindingReference{
						Path: providerCluster,
						Name: exportName,
					},
				},
			},
		}
		if err := wsClient.Create(ctx, binding); err != nil {
			return subroutines.Result{}, fmt.Errorf("creating verification binding %q: %w", exportName, err)
		}
		return subroutines.Pending(s.opts.RequeueInterval, "created verification binding"), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting verification binding %q: %w", exportName, err)
	}

	if binding.Status.Phase != kcpapisv1alpha2.APIBindingPhaseBound {
		return subroutines.Pending(s.opts.RequeueInterval, "waiting for verification binding to become bound"), nil
	}

	return subroutines.OK(), nil
}

// releaseWorksapce drops the finalizer for this AcceptAPI and deletes the workspace if it was the last finalizer.
func (s *bindingVerifiedSubroutine) releaseWorkspace(ctx context.Context, wsName, refFinalizer string) error {
	treeClient, err := s.treeRootClient()
	if err != nil {
		return err
	}

	workspace := &kcptenancyv1alpha1.Workspace{}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := treeClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: wsName}, workspace); err != nil {
			return err
		}
		if !controllerutil.RemoveFinalizer(workspace, refFinalizer) {
			return nil
		}
		return treeClient.Update(ctx, workspace)
	})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("removing reference finalizer from workspace %q: %w", wsName, err)
	}

	for _, finalizer := range workspace.Finalizers {
		if strings.HasPrefix(finalizer, refFinalizerPrefix) {
			return nil
		}
	}

	if err := treeClient.Delete(ctx, workspace); ctrlruntimeclient.IgnoreNotFound(err) != nil {
		return fmt.Errorf("deleting unreferenced workspace %q: %w", wsName, err)
	}
	return nil
}

func workspaceName(providerCluster, apiExportName string) string {
	return workspaceNamePrefix + names.Hash(providerCluster, apiExportName)
}

func referenceFinalizer(providerCluster string, acceptAPI *pmbrokerv1alpha1.AcceptAPI) string {
	return refFinalizerPrefix + names.Hash(providerCluster, acceptAPI.Name)
}
