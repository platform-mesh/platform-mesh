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

package wait

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// The checks: what the waits of this package wait for, as one pass. A check
// looks once and returns nil, or an error that says what is missing. The
// waits (WorkspaceReady and the like) call a check until it returns nil, and
// so does whoever has a loop of their own, like the steps of
// framework/steps.

// ErrNotReady is wrapped by the errors of the checks for an object that is
// there but not ready yet: checking again later can succeed.
var ErrNotReady = errors.New("not ready")

// CheckWorkspaceReady returns an error unless the Workspace name in cl's
// workspace is Ready.
func CheckWorkspaceReady(ctx context.Context, cl ctrlruntimeclient.Client, name string) error {
	ws := &kcptenancyv1alpha1.Workspace{}
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, ws); err != nil {
		return fmt.Errorf("getting workspace %s: %w", name, err)
	}
	if ws.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
		return fmt.Errorf("workspace %s: %w: phase %q:\n%s", name, ErrNotReady, ws.Status.Phase, toYAML(ws.Status))
	}
	return nil
}

// CheckAPIBindingBound returns an error unless the APIBinding name in cl's
// workspace is Bound.
func CheckAPIBindingBound(ctx context.Context, cl ctrlruntimeclient.Client, name string) error {
	binding := &kcpapisv1alpha2.APIBinding{}
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, binding); err != nil {
		return fmt.Errorf("getting APIBinding %s: %w", name, err)
	}
	if binding.Status.Phase != kcpapisv1alpha2.APIBindingPhaseBound {
		return fmt.Errorf("APIBinding %s: %w: phase %q:\n%s", name, ErrNotReady, binding.Status.Phase, toYAML(binding.Status.Conditions))
	}
	return nil
}

// CheckReady returns nil when the Ready condition is True, and otherwise an
// error wrapping ErrNotReady that lists the conditions.
func CheckReady(conds []metav1.Condition) error {
	if meta.IsStatusConditionTrue(conds, "Ready") {
		return nil
	}
	return fmt.Errorf("%w:\n%s", ErrNotReady, Conditions(conds))
}

// CheckReadyAt is CheckReady for an object whose controller records, in the
// Ready condition, the generation it has seen: Ready for what the object
// said before its last change does not count.
func CheckReadyAt(conds []metav1.Condition, generation int64) error {
	if ready := meta.FindStatusCondition(conds, "Ready"); ready != nil && ready.Status == metav1.ConditionTrue && ready.ObservedGeneration != generation {
		return fmt.Errorf("%w: Ready at generation %d, not yet at %d", ErrNotReady, ready.ObservedGeneration, generation)
	}
	return CheckReady(conds)
}

// CheckObjectReady is CheckReady for an object of any kind, typed or
// unstructured, that keeps conditions in status.conditions.
func CheckObjectReady(obj ctrlruntimeclient.Object) error {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return err
	}
	var status struct {
		Conditions []metav1.Condition `json:"conditions"`
	}
	raw, _, _ := unstructured.NestedMap(content, "status")
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &status); err != nil {
		return fmt.Errorf("the conditions of %s: %w", obj.GetName(), err)
	}
	if err := CheckReady(status.Conditions); err != nil {
		return fmt.Errorf("%s: %w", obj.GetName(), err)
	}
	return nil
}

// CheckGone turns the result of a Get into a check for the object to be
// deleted: nil when it was not found. what names the object.
func CheckGone(err error, what string) error {
	switch {
	case err == nil:
		return fmt.Errorf("%s is still there", what)
	case apierrors.IsNotFound(err):
		return nil
	default:
		return fmt.Errorf("%s: %w", what, err)
	}
}

// Conditions lists conditions one per line, for a message.
func Conditions(conds []metav1.Condition) string {
	if len(conds) == 0 {
		return "status.conditions is empty"
	}
	lines := make([]string, 0, len(conds))
	for _, c := range conds {
		lines = append(lines, fmt.Sprintf("type=%s status=%s reason=%s message=%s", c.Type, c.Status, c.Reason, c.Message))
	}
	return strings.Join(lines, "\n")
}
