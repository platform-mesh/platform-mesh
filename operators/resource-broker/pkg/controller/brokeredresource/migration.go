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

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/resource-broker/pkg/names"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

const (
	// MigrationFinalizer is placed on consumer objects to clean up
	// their Migration on deletion.
	MigrationFinalizer = "broker.platform-mesh.io/migration-ref"

	// migrationNamePrefix prefixes the hashed Migration names.
	migrationNamePrefix = "migration-"
)

// migrationSubroutine checks that the assigned provider still accepts the
// consumer object and drives a Migration to a new provider when it does not.
type migrationSubroutine struct {
	opts Options
}

var (
	_ subroutines.Processor = &migrationSubroutine{}
	_ subroutines.Finalizer = &migrationSubroutine{}
)

func (s *migrationSubroutine) GetName() string {
	return "Migration"
}

func (s *migrationSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{MigrationFinalizer}
}

func (s *migrationSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Unstructured, got %T", obj)
	}

	cluster, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("no cluster name in context")
	}
	consumerCluster := cluster.String()

	name := s.migrationName(consumerCluster, u)

	migration := &pmcoordbrokerv1alpha1.Migration{}
	err := s.opts.CoordinationClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, migration)
	switch {
	case apierrors.IsNotFound(err):
		return s.checkAssignment(ctx, consumerCluster, u)
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting Migration %q: %w", name, err)
	}

	if !migration.DeletionTimestamp.IsZero() {
		return subroutines.Pending(s.opts.RequeueInterval, "migration is terminating"), nil
	}

	if migration.Status.State == pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted {
		return s.finishMigration(ctx, migration, u)
	}

	return subroutines.Pending(s.opts.RequeueInterval, "waiting for migration to complete"), nil
}

// Finalize deletes the Migration and waits for it to be gone.
func (s *migrationSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Unstructured, got %T", obj)
	}

	cluster, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("no cluster name in context")
	}

	name := s.migrationName(cluster.String(), u)

	migration := &pmcoordbrokerv1alpha1.Migration{}
	err := s.opts.CoordinationClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, migration)
	switch {
	case apierrors.IsNotFound(err):
		return subroutines.OK(), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting Migration %q: %w", name, err)
	}

	if migration.DeletionTimestamp.IsZero() {
		if err := s.opts.CoordinationClient.Delete(ctx, migration); ctrlruntimeclient.IgnoreNotFound(err) != nil {
			return subroutines.Result{}, fmt.Errorf("deleting Migration %q: %w", name, err)
		}
	}

	return subroutines.Pending(s.opts.RequeueInterval, "waiting for migration to be deleted"), nil
}

// checkAssignment verifies that the assigned provider still accepts the consumer object and creates a Migration when it does not.
func (s *migrationSubroutine) checkAssignment(ctx context.Context, consumerCluster string, u *unstructured.Unstructured) (subroutines.Result, error) {
	assignment := &pmcoordbrokerv1alpha1.Assignment{}
	err := s.opts.CoordinationClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: s.assignmentNameFor(consumerCluster, u)}, assignment)
	switch {
	case apierrors.IsNotFound(err):
		// The assignment subroutine creates the Assignment.
		return subroutines.OK(), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting Assignment: %w", err)
	}

	if assignment.Status.Phase != pmcoordbrokerv1alpha1.AssignmentPhaseBound {
		return subroutines.OK(), nil
	}

	providerClient, err := s.opts.WorkspaceClientFunc(assignment.Spec.ProviderCluster)
	if err != nil {
		return subroutines.Result{}, fmt.Errorf("building client for provider cluster %q: %w", assignment.Spec.ProviderCluster, err)
	}

	acceptAPI := &pmbrokerv1alpha1.AcceptAPI{}
	err = providerClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: assignment.Spec.AcceptAPIName}, acceptAPI)
	switch {
	case apierrors.IsNotFound(err):
		// The provider withdrew the AcceptAPI; migrate away.
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting AcceptAPI %q: %w", assignment.Spec.AcceptAPIName, err)
	default:
		if ok, _ := acceptAPI.AppliesTo(s.opts.GVR, u); ok {
			return subroutines.OK(), nil
		}
	}

	return s.createMigration(ctx, consumerCluster, assignment, u)
}

// createMigration picks a new provider and creates the Migration.
func (s *migrationSubroutine) createMigration(ctx context.Context, consumerCluster string, assignment *pmcoordbrokerv1alpha1.Assignment, u *unstructured.Unstructured) (subroutines.Result, error) {
	refs, err := s.opts.ListAcceptAPIs(ctx)
	if err != nil {
		return subroutines.Result{}, fmt.Errorf("listing AcceptAPIs: %w", err)
	}

	var matching []AcceptAPIRef
	for _, ref := range refs {
		if ref.Cluster == assignment.Spec.ProviderCluster && ref.AcceptAPI.Name == assignment.Spec.AcceptAPIName {
			continue
		}
		if ok, _ := ref.AcceptAPI.AppliesTo(s.opts.GVR, u); ok {
			matching = append(matching, ref)
		}
	}
	if len(matching) == 0 {
		return subroutines.Pending(s.opts.RequeueInterval, "no matching AcceptAPI to migrate to"), nil
	}

	pick := s.opts.PickAcceptAPI(matching)

	gvk := metav1.GroupVersionKind{Group: s.opts.GVK.Group, Version: s.opts.GVK.Version, Kind: s.opts.GVK.Kind}
	migration := &pmcoordbrokerv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name: s.migrationName(consumerCluster, u),
		},
		Spec: pmcoordbrokerv1alpha1.MigrationSpec{
			Assignment: assignment.Name,
			From: pmcoordbrokerv1alpha1.MigrationTarget{
				GVK:             gvk,
				ProviderCluster: assignment.Spec.ProviderCluster,
				AcceptAPIName:   assignment.Spec.AcceptAPIName,
			},
			To: pmcoordbrokerv1alpha1.MigrationTarget{
				GVK:             gvk,
				ProviderCluster: pick.Cluster,
				AcceptAPIName:   pick.AcceptAPI.Name,
			},
		},
	}
	if err := s.opts.CoordinationClient.Create(ctx, migration); err != nil {
		return subroutines.Result{}, fmt.Errorf("creating Migration %q: %w", migration.Name, err)
	}

	return subroutines.Pending(s.opts.RequeueInterval, "created migration"), nil
}

// finishMigration deletes the stale copy in the from origin staging workspace and then the completed Migration.
func (s *migrationSubroutine) finishMigration(ctx context.Context, migration *pmcoordbrokerv1alpha1.Migration, u *unstructured.Unstructured) (subroutines.Result, error) {
	from := migration.Status.FromStagingWorkspace
	if from != "" && from != migration.Status.StagingWorkspace {
		fromClient, err := s.opts.WorkspaceClientFunc(s.opts.StagingTreeRoot + ":" + from)
		if err != nil {
			return subroutines.Result{}, fmt.Errorf("building client for staging workspace %q: %w", from, err)
		}

		oldCopy := &unstructured.Unstructured{}
		fromGVK := migration.Spec.From.GVK
		oldCopy.SetGroupVersionKind(schema.GroupVersionKind{Group: fromGVK.Group, Version: fromGVK.Version, Kind: fromGVK.Kind})
		nn := types.NamespacedName{Namespace: u.GetNamespace(), Name: u.GetName()}
		err = fromClient.Get(ctx, nn, oldCopy)
		switch {
		case apierrors.IsNotFound(err):
			// Old copy gone, proceed to delete the Migration.
		case err != nil:
			return subroutines.Result{}, fmt.Errorf("getting old staging copy: %w", err)
		default:
			if oldCopy.GetDeletionTimestamp().IsZero() {
				if err := fromClient.Delete(ctx, oldCopy); ctrlruntimeclient.IgnoreNotFound(err) != nil {
					return subroutines.Result{}, fmt.Errorf("deleting old staging copy: %w", err)
				}
			}
			return subroutines.Pending(s.opts.RequeueInterval, "waiting for old staging copy to be deleted"), nil
		}
	}

	if err := s.opts.CoordinationClient.Delete(ctx, migration); ctrlruntimeclient.IgnoreNotFound(err) != nil {
		return subroutines.Result{}, fmt.Errorf("deleting Migration %q: %w", migration.Name, err)
	}

	return subroutines.Pending(s.opts.RequeueInterval, "waiting for migration to be deleted"), nil
}

func (s *migrationSubroutine) migrationName(consumerCluster string, u *unstructured.Unstructured) string {
	return migrationName(consumerCluster, s.opts.GVR, u.GetNamespace(), u.GetName())
}

func (s *migrationSubroutine) assignmentNameFor(consumerCluster string, u *unstructured.Unstructured) string {
	return assignmentName(consumerCluster, s.opts.GVR, u.GetNamespace(), u.GetName())
}

func migrationName(consumerCluster string, gvr metav1.GroupVersionResource, namespace, name string) string {
	return migrationNamePrefix + names.Hash(consumerCluster, gvr.Group, gvr.Version, gvr.Resource, namespace, name)
}
