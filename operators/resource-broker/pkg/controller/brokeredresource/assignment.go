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

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

const (
	// AssignmentFinalizer is placed on consumer objects to clean up
	// their Assignment on deletion.
	AssignmentFinalizer = "broker.platform-mesh.io/assignment-ref"

	// assignmentNamePrefix prefixes the hashed Assignment names.
	assignmentNamePrefix = "assignment-"

	// migrationNamePrefix prefixes the hashed Migration names.
	migrationNamePrefix = "migration-"

	// stagingWorkspaceNamePrefix prefixes the hashed StagingWorkspace names.
	stagingWorkspaceNamePrefix = "staging-"
)

// assignmentSubroutine ensures the Assignment record exists and the StagingWorkspace for the consumer/provider combination exists.
// It also verifies the provider still applies and if not chooses a new provider, creates that StagingWorkspace and the Migration.
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

	origStatus := assignment.Status.DeepCopy()
	result, procErr := s.reconcileAssignment(ctx, consumerCluster, assignment, u)
	if !equality.Semantic.DeepEqual(origStatus, &assignment.Status) {
		if err := s.opts.CoordinationClient.Status().Update(ctx, assignment); err != nil {
			return subroutines.Result{}, fmt.Errorf("updating Assignment status %q: %w", name, err)
		}
	}
	return result, procErr
}

func (s *assignmentSubroutine) reconcileAssignment(ctx context.Context, consumerCluster string, assignment *pmcoordbrokerv1alpha1.Assignment, u *unstructured.Unstructured) (subroutines.Result, error) {
	migration := &pmcoordbrokerv1alpha1.Migration{}
	err := s.opts.CoordinationClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: s.migrationName(consumerCluster, u)}, migration)
	switch {
	case apierrors.IsNotFound(err):
		migration = nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting Migration %q: %w", migration.Name, err)
	}

	if migration != nil {
		if !migration.DeletionTimestamp.IsZero() {
			return subroutines.Pending(s.opts.RequeueInterval, "migration is terminating"), nil
		}
		if migration.Status.State != pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted {
			return subroutines.Pending(s.opts.RequeueInterval, "waiting for migration to complete"), nil
		}
		return s.finishMigration(ctx, assignment, migration, u)
	}

	if result, err := s.ensureBound(ctx, assignment); err != nil || result.Requeue() > 0 {
		return result, err
	}

	return s.validate(ctx, consumerCluster, assignment, u)
}

func (s *assignmentSubroutine) ensureBound(ctx context.Context, assignment *pmcoordbrokerv1alpha1.Assignment) (subroutines.Result, error) {
	if assignment.Status.ProviderCluster == "" || assignment.Status.AcceptAPIName == "" {
		assignment.Status.ProviderCluster = assignment.Spec.ProviderCluster
		assignment.Status.AcceptAPIName = assignment.Spec.AcceptAPIName
		assignment.Status.APIExportName = ""
	}

	if assignment.Status.APIExportName == "" {
		exportName, err := s.resolveAPIExportName(ctx, assignment.Status.ProviderCluster, assignment.Status.AcceptAPIName)
		if err != nil {
			return subroutines.Result{}, err
		}
		assignment.Status.APIExportName = exportName
	}

	wsName := stagingWorkspaceName(assignment.Spec.ConsumerCluster, assignment.Status.ProviderCluster, assignment.Status.APIExportName)
	assignment.Status.StagingWorkspace = wsName

	sw := &pmcoordbrokerv1alpha1.StagingWorkspace{}
	err := s.opts.CoordinationClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: wsName}, sw)
	switch {
	case apierrors.IsNotFound(err):
		sw = &pmcoordbrokerv1alpha1.StagingWorkspace{
			ObjectMeta: metav1.ObjectMeta{
				Name: wsName,
			},
			Spec: pmcoordbrokerv1alpha1.StagingWorkspaceSpec{
				ConsumerCluster: assignment.Spec.ConsumerCluster,
				ProviderCluster: assignment.Status.ProviderCluster,
				APIExportName:   assignment.Status.APIExportName,
			},
		}
		if err := s.opts.CoordinationClient.Create(ctx, sw); err != nil {
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

func (s *assignmentSubroutine) validate(ctx context.Context, consumerCluster string, assignment *pmcoordbrokerv1alpha1.Assignment, u *unstructured.Unstructured) (subroutines.Result, error) {
	providerClient, err := s.opts.WorkspaceClientFunc(assignment.Status.ProviderCluster)
	if err != nil {
		return subroutines.Result{}, fmt.Errorf("building client for provider cluster %q: %w", assignment.Status.ProviderCluster, err)
	}

	acceptAPI := &pmbrokerv1alpha1.AcceptAPI{}
	err = providerClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: assignment.Status.AcceptAPIName}, acceptAPI)
	switch {
	case apierrors.IsNotFound(err):
		// The provider withdrew the AcceptAPI; migrate away.
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting AcceptAPI %q: %w", assignment.Status.AcceptAPIName, err)
	default:
		if ok, _ := acceptAPI.AppliesTo(s.opts.GVR, u); ok {
			return subroutines.OK(), nil
		}
	}

	return s.startMigration(ctx, consumerCluster, assignment, u)
}

func (s *assignmentSubroutine) startMigration(ctx context.Context, consumerCluster string, assignment *pmcoordbrokerv1alpha1.Assignment, u *unstructured.Unstructured) (subroutines.Result, error) {
	refs, err := s.opts.ListAcceptAPIs(ctx)
	if err != nil {
		return subroutines.Result{}, fmt.Errorf("listing AcceptAPIs: %w", err)
	}

	var matching []AcceptAPIRef
	for _, ref := range refs {
		if ref.Cluster == assignment.Status.ProviderCluster && ref.AcceptAPI.Name == assignment.Status.AcceptAPIName {
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

	if assignment.Spec.ProviderCluster != pick.Cluster || assignment.Spec.AcceptAPIName != pick.AcceptAPI.Name {
		assignment.Spec.ProviderCluster = pick.Cluster
		assignment.Spec.AcceptAPIName = pick.AcceptAPI.Name
		if err := s.opts.CoordinationClient.Update(ctx, assignment); err != nil {
			return subroutines.Result{}, fmt.Errorf("repointing Assignment %q: %w", assignment.Name, err)
		}
	}

	destWorkspace := stagingWorkspaceName(assignment.Spec.ConsumerCluster, pick.Cluster, pick.AcceptAPI.Spec.APIExportName)
	sw := &pmcoordbrokerv1alpha1.StagingWorkspace{
		ObjectMeta: metav1.ObjectMeta{
			Name: destWorkspace,
		},
		Spec: pmcoordbrokerv1alpha1.StagingWorkspaceSpec{
			ConsumerCluster: assignment.Spec.ConsumerCluster,
			ProviderCluster: pick.Cluster,
			APIExportName:   pick.AcceptAPI.Spec.APIExportName,
		},
	}
	if err := s.opts.CoordinationClient.Create(ctx, sw); err != nil && !apierrors.IsAlreadyExists(err) {
		return subroutines.Result{}, fmt.Errorf("creating StagingWorkspace %q: %w", destWorkspace, err)
	}

	gvk := metav1.GroupVersionKind{Group: s.opts.GVK.Group, Version: s.opts.GVK.Version, Kind: s.opts.GVK.Kind}
	migration := &pmcoordbrokerv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name: s.migrationName(consumerCluster, u),
		},
		Spec: pmcoordbrokerv1alpha1.MigrationSpec{
			Assignment:           assignment.Name,
			Namespace:            u.GetNamespace(),
			Name:                 u.GetName(),
			FromStagingWorkspace: assignment.Status.StagingWorkspace,
			StagingWorkspace:     destWorkspace,
			From: pmcoordbrokerv1alpha1.MigrationTarget{
				GVK:             gvk,
				ProviderCluster: assignment.Status.ProviderCluster,
				AcceptAPIName:   assignment.Status.AcceptAPIName,
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

func (s *assignmentSubroutine) finishMigration(ctx context.Context, assignment *pmcoordbrokerv1alpha1.Assignment, migration *pmcoordbrokerv1alpha1.Migration, u *unstructured.Unstructured) (subroutines.Result, error) {
	// Repoint the assignment first so consumer traffic flows to the new
	// staging workspace before the old copy is removed. Otherwise the copy
	// subroutine keeps recreating the old copy from the stale status.
	assignment.Status.ProviderCluster = migration.Spec.To.ProviderCluster
	assignment.Status.AcceptAPIName = migration.Spec.To.AcceptAPIName
	assignment.Status.APIExportName = ""
	assignment.Status.StagingWorkspace = migration.Spec.StagingWorkspace
	assignment.Status.Phase = pmcoordbrokerv1alpha1.AssignmentPhaseBound

	from := migration.Spec.FromStagingWorkspace
	if from != "" && from != migration.Spec.StagingWorkspace {
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
			// Old copy gone, proceed with the cutover.
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

	if err := s.releaseStagingWorkspace(ctx, from, assignment.Name, migration.Name); err != nil {
		return subroutines.Result{}, err
	}

	return subroutines.Pending(s.opts.RequeueInterval, "waiting for migration to be deleted"), nil
}

func (s *assignmentSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Unstructured, got %T", obj)
	}

	cluster, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("no cluster name in context")
	}
	consumerCluster := cluster.String()

	assignmentName := s.assignmentName(consumerCluster, u)

	migration := &pmcoordbrokerv1alpha1.Migration{}
	err := s.opts.CoordinationClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: s.migrationName(consumerCluster, u)}, migration)
	switch {
	case apierrors.IsNotFound(err):
		// No migration to clean up.
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting Migration: %w", err)
	default:
		for _, wsName := range []string{migration.Spec.StagingWorkspace, migration.Spec.FromStagingWorkspace} {
			if wsName == "" {
				continue
			}
			if err := s.releaseStagingWorkspace(ctx, wsName, assignmentName, migration.Name); err != nil {
				return subroutines.Result{}, err
			}
		}
		if migration.DeletionTimestamp.IsZero() {
			if err := s.opts.CoordinationClient.Delete(ctx, migration); ctrlruntimeclient.IgnoreNotFound(err) != nil {
				return subroutines.Result{}, fmt.Errorf("deleting Migration %q: %w", migration.Name, err)
			}
		}
		return subroutines.Pending(s.opts.RequeueInterval, "waiting for migration to be deleted"), nil
	}

	assignment := &pmcoordbrokerv1alpha1.Assignment{}
	err = s.opts.CoordinationClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: assignmentName}, assignment)
	switch {
	case apierrors.IsNotFound(err):
		return subroutines.OK(), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("getting Assignment %q: %w", assignmentName, err)
	}

	if wsName := assignment.Status.StagingWorkspace; wsName != "" {
		if err := s.releaseStagingWorkspace(ctx, wsName, assignment.Name, ""); err != nil {
			return subroutines.Result{}, err
		}
	}

	if assignment.DeletionTimestamp.IsZero() {
		if err := s.opts.CoordinationClient.Delete(ctx, assignment); ctrlruntimeclient.IgnoreNotFound(err) != nil {
			return subroutines.Result{}, fmt.Errorf("deleting Assignment %q: %w", assignmentName, err)
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

// resolveAPIExportName reads the APIExport name from the AcceptAPI in the provider workspace.
func (s *assignmentSubroutine) resolveAPIExportName(ctx context.Context, providerCluster, acceptAPIName string) (string, error) {
	providerClient, err := s.opts.WorkspaceClientFunc(providerCluster)
	if err != nil {
		return "", fmt.Errorf("building client for provider cluster %q: %w", providerCluster, err)
	}

	acceptAPI := &pmbrokerv1alpha1.AcceptAPI{}
	if err := providerClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: acceptAPIName}, acceptAPI); err != nil {
		return "", fmt.Errorf("getting AcceptAPI %q in provider cluster %q: %w", acceptAPIName, providerCluster, err)
	}

	return acceptAPI.Spec.APIExportName, nil
}

// releaseStagingWorkspace deletes the StagingWorkspace unless another Assignment or Migration still references it.
func (s *assignmentSubroutine) releaseStagingWorkspace(ctx context.Context, wsName, ownAssignment, ownMigration string) error {
	assignments := &pmcoordbrokerv1alpha1.AssignmentList{}
	if err := s.opts.CoordinationClient.List(ctx, assignments); err != nil {
		return fmt.Errorf("listing Assignments: %w", err)
	}
	for _, other := range assignments.Items {
		if other.Name == ownAssignment || !other.DeletionTimestamp.IsZero() {
			continue
		}
		if other.Status.StagingWorkspace == wsName {
			return nil
		}
	}

	migrations := &pmcoordbrokerv1alpha1.MigrationList{}
	if err := s.opts.CoordinationClient.List(ctx, migrations); err != nil {
		return fmt.Errorf("listing Migrations: %w", err)
	}
	for _, migration := range migrations.Items {
		if migration.Name == ownMigration || !migration.DeletionTimestamp.IsZero() {
			continue
		}
		if migration.Spec.StagingWorkspace == wsName || migration.Spec.FromStagingWorkspace == wsName {
			return nil
		}
	}

	sw := &pmcoordbrokerv1alpha1.StagingWorkspace{
		ObjectMeta: metav1.ObjectMeta{
			Name: wsName,
		},
	}
	if err := s.opts.CoordinationClient.Delete(ctx, sw); ctrlruntimeclient.IgnoreNotFound(err) != nil {
		return fmt.Errorf("deleting unreferenced StagingWorkspace %q: %w", wsName, err)
	}

	return nil
}

func (s *assignmentSubroutine) assignmentName(consumerCluster string, u *unstructured.Unstructured) string {
	return assignmentName(consumerCluster, s.opts.GVR, u.GetNamespace(), u.GetName())
}

func (s *assignmentSubroutine) migrationName(consumerCluster string, u *unstructured.Unstructured) string {
	return migrationName(consumerCluster, s.opts.GVR, u.GetNamespace(), u.GetName())
}

func assignmentName(consumerCluster string, gvr metav1.GroupVersionResource, namespace, name string) string {
	return assignmentNamePrefix + names.Hash(consumerCluster, gvr.Group, gvr.Version, gvr.Resource, namespace, name)
}

func migrationName(consumerCluster string, gvr metav1.GroupVersionResource, namespace, name string) string {
	return migrationNamePrefix + names.Hash(consumerCluster, gvr.Group, gvr.Version, gvr.Resource, namespace, name)
}

// stagingWorkspaceName derives the deterministic StagingWorkspace name for a
// consumer/provider/APIExport tuple.
func stagingWorkspaceName(consumerCluster, providerCluster, apiExportName string) string {
	return stagingWorkspaceNamePrefix + names.Hash(consumerCluster, providerCluster, apiExportName)
}
