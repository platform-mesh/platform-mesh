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

package restore

import (
	"context"
	"fmt"
	"time"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/velero"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	veleroRestoreSubroutineName     = "velero-restore"
	conditionVeleroRestoreCompleted = "VeleroRestoreCompleted"
	credentialRestoreSubroutineName = "credential-restore"
	credentialRestoreLabel          = "platform-mesh.io/restore-credentials"
)

type VeleroRestoreSubroutine struct {
	name   string
	client ctrlruntimeclient.Client
}

type CredentialRestoreSubroutine struct {
	name   string
	client ctrlruntimeclient.Client
}

func NewCredentialRestoreSubroutine(cli ctrlruntimeclient.Client) *CredentialRestoreSubroutine {
	return &CredentialRestoreSubroutine{name: credentialRestoreSubroutineName, client: cli}
}

func (c *CredentialRestoreSubroutine) GetName() string {
	return c.name
}

func NewVeleroRestoreSubroutine(cli ctrlruntimeclient.Client) *VeleroRestoreSubroutine {
	return &VeleroRestoreSubroutine{
		name:   veleroRestoreSubroutineName,
		client: cli,
	}
}

func (v *VeleroRestoreSubroutine) GetName() string {
	return v.name
}

func (v *VeleroRestoreSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	platformRestore, ok := obj.(*pmbackupv1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected a pmbackupv1alpha1.PlatformRestore, got %T", obj)
	}

	if restoreTerminal(platformRestore) {
		return subroutines.OK(), false, nil
	}

	if conditionIsTrue(platformRestore, conditionVeleroRestoreCompleted) {
		return subroutines.OK(), false, nil
	}

	if changed := setPhase(platformRestore, pmbackupv1alpha1.RestorePhaseRestoringVelero); changed {
		return subroutines.StopWithRequeue(time.Second, "phase set to RestoringVelero"), true, nil
	}

	statusChanged := false
	log := logger.LoadLoggerFromContext(ctx)

	backupName := fmt.Sprintf("%s-platform-mesh", platformRestore.Spec.Source.BackupID)

	log.Info().
		Str("subroutine", veleroRestoreSubroutineName).
		Str("platformRestore", platformRestore.Name).
		Str("veleroBackup", backupName).
		Msg("starting Velero restore")

	if err := velero.NewStorageLocation(v.client).EnsureForRestore(ctx, *platformRestore); err != nil {
		return subroutines.OK(), statusChanged, fmt.Errorf("failed to ensure Velero BackupStorageLocation: %w", err)
	}

	available, err := v.backupStorageLocationAvailable(ctx)
	if err != nil {
		return subroutines.OK(), statusChanged, err
	}

	if !available {
		return subroutines.StopWithRequeue(5*time.Second, "waiting for Velero BackupStorageLocation/default to become available"), statusChanged, nil
	}

	backupReady, err := v.veleroBackupReady(ctx, backupName)
	if err != nil {
		return subroutines.OK(), statusChanged, err
	}

	if !backupReady {
		log.Info().
			Str("subroutine", veleroRestoreSubroutineName).
			Str("platformRestore", platformRestore.Name).
			Str("veleroBackup", backupName).
			Msg("waiting for Velero Backup to exist and complete")

		return subroutines.StopWithRequeue(
			5*time.Second,
			fmt.Sprintf("waiting for Velero Backup %s/%s", velero.DefaultNamespace, backupName),
		), statusChanged, nil
	}

	current, err := velero.NewRestore(v.client).Ensure(ctx, *platformRestore, backupName)
	if err != nil {
		return subroutines.OK(), statusChanged, fmt.Errorf("failed to ensure Velero Restore: %w", err)
	}

	switch current.Status.Phase {
	case velerov1.RestorePhaseCompleted:
		if markVeleroRestoreCompleted(platformRestore) {
			statusChanged = true
		}
		log.Info().
			Str("subroutine", veleroRestoreSubroutineName).
			Str("platformRestore", platformRestore.Name).
			Str("veleroRestore", current.Name).
			Int("warnings", current.Status.Warnings).
			Msg("Velero Restore completed")
		return subroutines.OK(), statusChanged, nil

	case velerov1.RestorePhaseFailed, velerov1.RestorePhasePartiallyFailed, velerov1.RestorePhaseFailedValidation:
		return subroutines.OK(), statusChanged, fmt.Errorf("failure Velero Restore %s/%s reached phase %s", current.Namespace, current.Name, current.Status.Phase)

	default:
		log.Info().
			Str("subroutine", veleroRestoreSubroutineName).
			Str("platformRestore", platformRestore.Name).
			Str("veleroRestore", current.Name).
			Str("phase", string(current.Status.Phase)).
			Msg("Velero Restore not complete yet")
		return subroutines.StopWithRequeue(5*time.Second, fmt.Sprintf("waiting for Velero Restore %s/%s", current.Namespace, current.Name)), statusChanged, nil
	}
}

func (v *VeleroRestoreSubroutine) backupStorageLocationAvailable(ctx context.Context) (bool, error) {
	var location velerov1.BackupStorageLocation

	err := v.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: velero.DefaultNamespace, Name: "default"}, &location)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get Velero BackupStorageLocation %s/default: %w", velero.DefaultNamespace, err)
	}
	return location.Status.Phase == velerov1.BackupStorageLocationPhaseAvailable, nil
}

func (v *VeleroRestoreSubroutine) veleroBackupReady(ctx context.Context, name string) (bool, error) {
	var backup velerov1.Backup

	err := v.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: velero.DefaultNamespace, Name: name}, &backup)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get Velero Backup %s/%s: %w", velero.DefaultNamespace, name, err)
	}

	switch backup.Status.Phase {
	case velerov1.BackupPhaseCompleted:
		return true, nil

	case velerov1.BackupPhaseFailed, velerov1.BackupPhasePartiallyFailed, velerov1.BackupPhaseFailedValidation:
		return false, fmt.Errorf("velero Backup %s/%s reached phase %s", backup.Namespace, backup.Name, backup.Status.Phase)

	default:
		return false, nil
	}
}

func markVeleroRestoreCompleted(platformRestore *pmbackupv1alpha1.PlatformRestore) bool {
	return markCondition(
		platformRestore,
		conditionVeleroRestoreCompleted,
		"VeleroRestoreCompleted",
		"Velero restore completed successfully",
	)
}

func (c *CredentialRestoreSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	pr, ok := obj.(*pmbackupv1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected pmbackupv1alpha1.PlatformRestore, got %T", obj)
	}

	if restoreTerminal(pr) || conditionIsTrue(pr, conditionCredentialsRestored) {
		return subroutines.OK(), false, nil
	}

	if changed := setPhase(pr, pmbackupv1alpha1.RestorePhaseRestoringCredentials); changed {
		return subroutines.StopWithRequeue(time.Second, "phase set to RestoringCredentials"), true, nil
	}

	if err := velero.NewStorageLocation(c.client).EnsureForRestore(ctx, *pr); err != nil {
		return subroutines.OK(), false, fmt.Errorf("failed to ensure Velero BackupStorageLocation for credential restore: %w", err)
	}

	backupName := fmt.Sprintf("%s-platform-mesh", pr.Spec.Source.BackupID)
	backupReady, err := c.veleroBackupReady(ctx, backupName)
	if err != nil {
		return subroutines.OK(), false, err
	}
	if !backupReady {
		return subroutines.StopWithRequeue(5*time.Second, fmt.Sprintf("waiting for Velero backup %s", backupName)), false, nil
	}

	restoreName := fmt.Sprintf("%s-credentials", pr.Name)
	restore, err := c.ensureCredentialRestore(ctx, restoreName, backupName)
	if err != nil {
		return subroutines.OK(), false, err
	}

	phase, _, _ := unstructured.NestedString(restore.Object, "status", "phase")
	switch phase {
	case "Completed":
		statusChanged := markCondition(
			pr,
			conditionCredentialsRestored,
			"CredentialsRestored",
			"Identity and kcp credential Secrets/ConfigMaps were restored from Velero",
		)
		return subroutines.OK(), statusChanged, nil
	case "Failed", "PartiallyFailed", "FailedValidation":
		return subroutines.OK(), false, fmt.Errorf("credential Velero Restore %s/%s failed with phase %s", velero.DefaultNamespace, restoreName, phase)
	default:
		return subroutines.StopWithRequeue(5*time.Second, fmt.Sprintf("waiting for credential restore %s/%s phase=%s", velero.DefaultNamespace, restoreName, phase)), false, nil
	}
}

func (c *CredentialRestoreSubroutine) ensureCredentialRestore(ctx context.Context, name, backupName string) (*unstructured.Unstructured, error) {
	restore := &unstructured.Unstructured{}
	restore.SetAPIVersion("velero.io/v1")
	restore.SetKind("Restore")

	err := c.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: velero.DefaultNamespace, Name: name}, restore)
	if err == nil {
		return restore, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get credential Velero Restore %s/%s: %w", velero.DefaultNamespace, name, err)
	}

	restore = &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "velero.io/v1",
		"kind":       "Restore",
		"metadata": map[string]any{
			"name":      name,
			"namespace": velero.DefaultNamespace,
			"labels": map[string]any{
				"backup.platform-mesh.io/restore-part": "credentials",
			},
		},
		"spec": map[string]any{
			"backupName":              backupName,
			"existingResourcePolicy":  "update",
			"includedNamespaces":      []any{platformMeshNamespace},
			"includedResources":       []any{"secrets", "configmaps"},
			"restorePVs":              false,
			"includeClusterResources": false,
			"labelSelector": map[string]any{
				"matchLabels": map[string]any{credentialRestoreLabel: "true"},
			},
		},
	}}

	if err := c.client.Create(ctx, restore); err != nil {
		return nil, fmt.Errorf("failed to create credential Velero Restore %s/%s: %w", velero.DefaultNamespace, name, err)
	}
	return restore, nil
}

func (c *CredentialRestoreSubroutine) veleroBackupReady(ctx context.Context, name string) (bool, error) {
	backup := &unstructured.Unstructured{}
	backup.SetAPIVersion("velero.io/v1")
	backup.SetKind("Backup")

	err := c.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: velero.DefaultNamespace, Name: name}, backup)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get Velero Backup %s/%s: %w", velero.DefaultNamespace, name, err)
	}

	phase, _, _ := unstructured.NestedString(backup.Object, "status", "phase")
	switch phase {
	case "Completed":
		return true, nil
	case "Failed", "PartiallyFailed", "FailedValidation":
		return false, fmt.Errorf("velero Backup %s/%s failed with phase %s", velero.DefaultNamespace, name, phase)
	default:
		return false, nil
	}
}
