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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

// bindingReadySubroutine binds the provider's APIExport in the staging workspace.
type bindingReadySubroutine struct {
	opts Options
}

var _ subroutines.Processor = &bindingReadySubroutine{}

func (s *bindingReadySubroutine) GetName() string {
	return pmcoordbrokerv1alpha1.StagingWorkspaceConditionBindingReady
}

func (s *bindingReadySubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	sw, ok := obj.(*pmcoordbrokerv1alpha1.StagingWorkspace)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected StagingWorkspace, got %T", obj)
	}

	claims, err := s.providerPermissionClaims(ctx, sw)
	if err != nil {
		return subroutines.Result{}, err
	}

	wsPath := s.opts.StagingTreeRoot + ":" + sw.Name
	wsClient, err := s.opts.WorkspaceClientFunc(wsPath)
	if err != nil {
		return subroutines.Result{}, fmt.Errorf("building client for workspace %q: %w", wsPath, err)
	}

	binding := &kcpapisv1alpha2.APIBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: sw.Spec.APIExportName,
		},
	}
	res, err := controllerutil.CreateOrUpdate(ctx, wsClient, binding, func() error {
		binding.Spec.Reference = kcpapisv1alpha2.BindingReference{
			Export: &kcpapisv1alpha2.ExportBindingReference{
				Path: sw.Spec.ProviderCluster,
				Name: sw.Spec.APIExportName,
			},
		}
		binding.Spec.PermissionClaims = claims
		return nil
	})
	if err != nil {
		return subroutines.Result{}, fmt.Errorf("ensuring staging binding %q: %w", sw.Spec.APIExportName, err)
	}
	if res != controllerutil.OperationResultNone {
		sw.Status.Phase = pmcoordbrokerv1alpha1.StagingWorkspacePhasePending
		return subroutines.Pending(s.opts.RequeueInterval, "configured staging binding"), nil
	}

	if binding.Status.Phase != kcpapisv1alpha2.APIBindingPhaseBound {
		sw.Status.Phase = pmcoordbrokerv1alpha1.StagingWorkspacePhasePending
		return subroutines.Pending(s.opts.RequeueInterval, "waiting for staging binding to become bound"), nil
	}

	sw.Status.Phase = pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady
	return subroutines.OK(), nil
}

// providerPermissionClaims mirrors the provider APIExport's permission claims as accepted claims.
func (s *bindingReadySubroutine) providerPermissionClaims(ctx context.Context, sw *pmcoordbrokerv1alpha1.StagingWorkspace) ([]kcpapisv1alpha2.AcceptablePermissionClaim, error) {
	providerClient, err := s.opts.WorkspaceClientFunc(sw.Spec.ProviderCluster)
	if err != nil {
		return nil, fmt.Errorf("building client for provider cluster %q: %w", sw.Spec.ProviderCluster, err)
	}

	export := &kcpapisv1alpha2.APIExport{}
	if err := providerClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: sw.Spec.APIExportName}, export); err != nil {
		return nil, fmt.Errorf("getting APIExport %q from provider cluster %q: %w", sw.Spec.APIExportName, sw.Spec.ProviderCluster, err)
	}

	if len(export.Spec.PermissionClaims) == 0 {
		return nil, nil
	}

	claims := make([]kcpapisv1alpha2.AcceptablePermissionClaim, 0, len(export.Spec.PermissionClaims))
	for _, pc := range export.Spec.PermissionClaims {
		claims = append(claims, kcpapisv1alpha2.AcceptablePermissionClaim{
			ScopedPermissionClaim: kcpapisv1alpha2.ScopedPermissionClaim{
				PermissionClaim: pc,
				Selector: kcpapisv1alpha2.PermissionClaimSelector{
					MatchAll: true,
				},
			},
			State: kcpapisv1alpha2.ClaimAccepted,
		})
	}
	return claims, nil
}
