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
	"go.platform-mesh.io/resource-broker/pkg/kubernetes"
	"go.platform-mesh.io/resource-broker/pkg/sync"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

const (
	// CopyFinalizer is placed on consumer objects to clean up their
	// staging copy on deletion.
	CopyFinalizer = "broker.platform-mesh.io/staging-copy"

	// ConsumerClusterAnnotation marks a staging copy with the logical
	// cluster name of the consumer object.
	ConsumerClusterAnnotation = "broker.platform-mesh.io/consumer-cluster"

	// ConsumerNameAnnotation marks a staging copy with the name of the
	// consumer object.
	ConsumerNameAnnotation = "broker.platform-mesh.io/consumer-name"
)

// copySubroutine copies the consumer object into the staging workspace and
// reflects the status back.
type copySubroutine struct {
	opts Options
}

var (
	_ subroutines.Processor = &copySubroutine{}
	_ subroutines.Finalizer = &copySubroutine{}
)

func (s *copySubroutine) GetName() string {
	return "Copy"
}

func (s *copySubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{CopyFinalizer}
}

func (s *copySubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Unstructured, got %T", obj)
	}

	cluster, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("no cluster name in context")
	}
	consumerCluster := cluster.String()

	consumerClient, err := subroutines.ClientFromContext(ctx)
	if err != nil {
		return subroutines.Result{}, err
	}

	// If a migration is active the consumer spec must be forward to the new provider.
	// TODO(ntnn): Ideally there's a validation webhook that prevents
	// spec changes after a migration has been kicked off.
	migration, err := s.activeMigration(ctx, consumerCluster, u)
	if err != nil {
		return subroutines.Result{}, err
	}
	if migration != nil {
		return s.copyToMigrationTarget(ctx, migration, u, consumerClient, consumerCluster)
	}

	stagingClient, result, err := stagingClient(ctx, s.opts, consumerCluster, u)
	if stagingClient == nil {
		return result, err
	}

	if u.GetNamespace() != "" {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: u.GetNamespace()}}
		if err := stagingClient.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
			return subroutines.Result{}, fmt.Errorf("creating namespace %q in staging workspace: %w", ns.Name, err)
		}
	}

	nn := types.NamespacedName{Namespace: u.GetNamespace(), Name: u.GetName()}
	if _, err := sync.Resource(ctx, s.opts.GVK, nn, nn, consumerClient, stagingClient); err != nil {
		return subroutines.Result{}, fmt.Errorf("copying resource to staging workspace: %w", err)
	}

	if err := s.annotateCopy(ctx, stagingClient, nn, consumerCluster); err != nil {
		return subroutines.Result{}, err
	}

	return subroutines.OKWithRequeue(s.opts.RequeueInterval), nil
}

// Finalize deletes the staging copy and waits for it to be gone before the
// assignment subroutine releases the Assignment.
func (s *copySubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Unstructured, got %T", obj)
	}

	cluster, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("no cluster name in context")
	}

	stagingClient, result, err := stagingClient(ctx, s.opts, cluster.String(), u)
	if stagingClient == nil {
		if err != nil {
			return result, err
		}
		// No bound assignment, nothing staged.
		return subroutines.OK(), nil
	}

	stagingCopy := &unstructured.Unstructured{}
	stagingCopy.SetGroupVersionKind(s.opts.GVK)
	nn := types.NamespacedName{Namespace: u.GetNamespace(), Name: u.GetName()}
	err = stagingClient.Get(ctx, nn, stagingCopy)
	switch {
	case apierrors.IsNotFound(err):
		return subroutines.OK(), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting staging copy: %w", err)
	}

	if stagingCopy.GetDeletionTimestamp().IsZero() {
		if err := stagingClient.Delete(ctx, stagingCopy); ctrlruntimeclient.IgnoreNotFound(err) != nil {
			return subroutines.Result{}, fmt.Errorf("deleting staging copy: %w", err)
		}
	}

	return subroutines.StopWithRequeue(s.opts.RequeueInterval, "waiting for staging copy to be deleted"), nil
}

// stagingClient resolves the staging workspace client via the bound
// Assignment. A nil client means the assignment is not bound yet; result and
// err carry the outcome.
func stagingClient(ctx context.Context, opts Options, consumerCluster string, u *unstructured.Unstructured) (ctrlruntimeclient.Client, subroutines.Result, error) {
	name := assignmentName(consumerCluster, opts.GVR, u.GetNamespace(), u.GetName())

	assignment := &pmcoordbrokerv1alpha1.Assignment{}
	err := opts.CoordinationClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, assignment)
	switch {
	case apierrors.IsNotFound(err):
		return nil, subroutines.Pending(opts.RequeueInterval, "waiting for assignment"), nil
	case err != nil:
		return nil, subroutines.Result{}, fmt.Errorf("getting Assignment %q: %w", name, err)
	}

	if assignment.Status.Phase != pmcoordbrokerv1alpha1.AssignmentPhaseBound || assignment.Status.StagingWorkspace == "" {
		return nil, subroutines.Pending(opts.RequeueInterval, "waiting for assignment to be bound"), nil
	}

	cl, err := opts.WorkspaceClientFunc(opts.StagingTreeRoot + ":" + assignment.Status.StagingWorkspace)
	if err != nil {
		return nil, subroutines.Result{}, fmt.Errorf("building client for staging workspace %q: %w", assignment.Status.StagingWorkspace, err)
	}
	return cl, subroutines.Result{}, nil
}

// annotateCopy records the consumer origin on the staging copy.
func (s *copySubroutine) annotateCopy(ctx context.Context, stagingClient ctrlruntimeclient.Client, nn types.NamespacedName, consumerCluster string) error {
	stagingCopy := &unstructured.Unstructured{}
	stagingCopy.SetGroupVersionKind(s.opts.GVK)
	if err := stagingClient.Get(ctx, nn, stagingCopy); err != nil {
		return fmt.Errorf("getting staging copy: %w", err)
	}

	anns := stagingCopy.GetAnnotations()
	if anns[ConsumerClusterAnnotation] == consumerCluster && anns[ConsumerNameAnnotation] == nn.Name {
		return nil
	}

	kubernetes.SetAnnotation(stagingCopy, ConsumerClusterAnnotation, consumerCluster)
	kubernetes.SetAnnotation(stagingCopy, ConsumerNameAnnotation, nn.Name)
	if err := stagingClient.Update(ctx, stagingCopy); err != nil {
		return fmt.Errorf("annotating staging copy: %w", err)
	}
	return nil
}

// activeMigration returns the active Migration if there is one.
func (s *copySubroutine) activeMigration(ctx context.Context, consumerCluster string, u *unstructured.Unstructured) (*pmcoordbrokerv1alpha1.Migration, error) {
	name := migrationName(consumerCluster, s.opts.GVR, u.GetNamespace(), u.GetName())

	migration := &pmcoordbrokerv1alpha1.Migration{}
	err := s.opts.CoordinationClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, migration)
	switch {
	case apierrors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("getting Migration %q: %w", name, err)
	}

	if migration.Status.State == pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted {
		return nil, nil
	}
	return migration, nil
}

// copyToMigrationTarget copies the consumer spec to the new provider staging workspace.
func (s *copySubroutine) copyToMigrationTarget(ctx context.Context, migration *pmcoordbrokerv1alpha1.Migration, u *unstructured.Unstructured, consumerClient ctrlruntimeclient.Client, consumerCluster string) (subroutines.Result, error) {
	sw := &pmcoordbrokerv1alpha1.StagingWorkspace{}
	err := s.opts.CoordinationClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: migration.Spec.StagingWorkspace}, sw)
	switch {
	case apierrors.IsNotFound(err):
		return subroutines.Pending(s.opts.RequeueInterval, "waiting for migration target staging workspace"), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting StagingWorkspace %q: %w", migration.Spec.StagingWorkspace, err)
	}
	if sw.Status.Phase != pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady {
		return subroutines.Pending(s.opts.RequeueInterval, "waiting for migration target staging workspace"), nil
	}

	targetClient, err := s.opts.WorkspaceClientFunc(s.opts.StagingTreeRoot + ":" + migration.Spec.StagingWorkspace)
	if err != nil {
		return subroutines.Result{}, fmt.Errorf("building client for staging workspace %q: %w", migration.Spec.StagingWorkspace, err)
	}

	if u.GetNamespace() != "" {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: u.GetNamespace()}}
		if err := targetClient.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
			return subroutines.Result{}, fmt.Errorf("creating namespace %q in staging workspace: %w", ns.Name, err)
		}
	}

	nn := types.NamespacedName{Namespace: u.GetNamespace(), Name: u.GetName()}
	if _, err := sync.Spec(ctx, s.opts.GVK, nn, nn, consumerClient, targetClient); err != nil {
		return subroutines.Result{}, fmt.Errorf("copying resource to migration target: %w", err)
	}

	if err := s.annotateCopy(ctx, targetClient, nn, consumerCluster); err != nil {
		return subroutines.Result{}, err
	}
	return subroutines.OKWithRequeue(s.opts.RequeueInterval), nil
}
