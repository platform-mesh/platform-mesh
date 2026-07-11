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

package migration

import (
	"context"
	"fmt"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// cutoverSubroutine completes the migration once the target staging copy
// is available.
type cutoverSubroutine struct {
	opts Options
}

var _ subroutines.Processor = &cutoverSubroutine{}

func (s *cutoverSubroutine) GetName() string {
	return pmcoordbrokerv1alpha1.MigrationConditionCutoverCompleted
}

func (s *cutoverSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	migration, ok := obj.(*pmcoordbrokerv1alpha1.Migration)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Migration, got %T", obj)
	}

	if migration.Status.State == pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted {
		return subroutines.OK(), nil
	}
	if migration.Status.State != pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress {
		return subroutines.Pending(s.opts.RequeueInterval, "waiting for stages to complete"), nil
	}

	stagingClient, err := s.opts.WorkspaceClientFunc(s.opts.StagingTreeRoot + ":" + migration.Spec.StagingWorkspace)
	if err != nil {
		return subroutines.Result{}, fmt.Errorf("building client for staging workspace %q: %w", migration.Spec.StagingWorkspace, err)
	}

	target := &unstructured.Unstructured{}
	target.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   migration.Spec.To.GVK.Group,
		Version: migration.Spec.To.GVK.Version,
		Kind:    migration.Spec.To.GVK.Kind,
	})
	nn := types.NamespacedName{Namespace: migration.Spec.Namespace, Name: migration.Spec.Name}
	err = stagingClient.Get(ctx, nn, target)
	switch {
	case apierrors.IsNotFound(err):
		return subroutines.Pending(s.opts.RequeueInterval, "waiting for staging copy in target workspace"), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting staging copy %q: %w", nn, err)
	}

	status, _, err := unstructured.NestedString(target.Object, "status", "status")
	if err != nil {
		return subroutines.Result{}, fmt.Errorf("reading status of staging copy %q: %w", nn, err)
	}
	if status != string(pmbrokerv1alpha1.StatusAvailable) {
		return subroutines.Pending(s.opts.RequeueInterval, fmt.Sprintf("waiting for staging copy to become available, currently %q", status)), nil
	}

	migration.Status.State = pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted
	return subroutines.OK(), nil
}
