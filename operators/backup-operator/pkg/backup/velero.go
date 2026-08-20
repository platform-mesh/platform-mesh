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

package backup

import (
	"context"
	"fmt"
	"time"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/velero"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	veleroSubroutineName = "velero-capture"
)

type VeleroCaptureSubroutine struct {
	name              string
	includedNamespace string
	client            ctrlruntimeclient.Client
}

func NewVeleroCaptureSubroutine(includedNamespace string, cli ctrlruntimeclient.Client) *VeleroCaptureSubroutine {
	return &VeleroCaptureSubroutine{
		name:              veleroSubroutineName,
		includedNamespace: includedNamespace,
		client:            cli,
	}
}

func (v *VeleroCaptureSubroutine) GetName() string {
	return v.name
}

func (v *VeleroCaptureSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	backup, ok := obj.(*pmbackupv1alpha1.PlatformBackup)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected a pmbackupv1alpha1.PlatformBackup, got a %T", obj)
	}

	log := logger.LoadLoggerFromContext(ctx)

	if !backup.Spec.Components.Velero.Enabled {
		log.Info().
			Str("subroutine", veleroSubroutineName).
			Str("platformBackup", backup.Name).
			Bool("veleroBackupEnabled", backup.Spec.Components.Velero.Enabled).
			Msg("Velero backup is not enabled on PlatformBackup CR")
		return subroutines.OK(), false, nil
	}

	log.Info().
		Str("subroutine", veleroSubroutineName).
		Str("platformBackup", backup.Name).
		Msg("ensuring Velero BackupStorageLocation")

	locationAvailable, err := velero.NewStorageLocation(v.client).EnsureAvailableForBackup(ctx, *backup)
	if err != nil {
		log.Error().
			Str("subroutine", veleroSubroutineName).
			Str("platformBackup", backup.Name).
			Err(err).
			Msg("failed to ensure Velero BackupStorageLocation")
		return subroutines.OK(), false, fmt.Errorf("failed to ensure Velero BackupStorageLocation: %w", err)
	}
	if !locationAvailable {
		log.Info().
			Str("subroutine", veleroSubroutineName).
			Str("platformBackup", backup.Name).
			Msg("waiting for Velero BackupStorageLocation to become available")
		return subroutines.StopWithRequeue(
			3*time.Second,
			"waiting for Velero BackupStorageLocation default to become available",
		), false, nil
	}

	if backup.Status.Artefacts.Velero != nil && backup.Status.Artefacts.Velero.BackupName != "" {
		log.Info().
			Str("subroutine", veleroSubroutineName).
			Str("platformBackup", backup.Name).
			Str("veleroBackup", backup.Status.Artefacts.Velero.BackupName).
			Msg("Velero backup already completed")
		return subroutines.OK(), false, nil
	}

	log.Info().
		Str("subroutine", veleroSubroutineName).
		Str("platformBackup", backup.Name).
		Str("includedNamespace", v.includedNamespace).
		Msg("ensuring Velero Backup")

	current, err := velero.NewBackup(v.client).Ensure(ctx, *backup, []string{v.includedNamespace})
	if err != nil {
		log.Error().
			Str("subroutine", veleroSubroutineName).
			Str("platformBackup", backup.Name).
			Err(err).
			Msg("failed to ensure Velero Backup")
		return subroutines.OK(), false, fmt.Errorf("failed to ensure Velero Backup: %w", err)
	}

	statusChanged := false
	switch current.Status.Phase {
	case velerov1.BackupPhaseCompleted:
		if backup.Status.Artefacts.Velero == nil {
			backup.Status.Artefacts.Velero = &pmbackupv1alpha1.VeleroArtefact{}
			statusChanged = true
		}

		if backup.Status.Artefacts.Velero.BackupName != current.Name {
			backup.Status.Artefacts.Velero.BackupName = current.Name
			statusChanged = true
		}

		log.Info().
			Str("subroutine", veleroSubroutineName).
			Str("platformBackup", backup.Name).
			Str("veleroBackup", current.Name).
			Msg("Velero Backup completed")

	case velerov1.BackupPhaseFailedValidation:
		if err := v.client.Delete(ctx, current); err != nil {
			return subroutines.OK(), false, fmt.Errorf("delete failed-validation Velero Backup %s/%s: %w", current.Namespace, current.Name, err)
		}
		log.Warn().
			Str("subroutine", veleroSubroutineName).
			Str("platformBackup", backup.Name).
			Str("veleroBackup", current.Name).
			Strs("validationErrors", current.Status.ValidationErrors).
			Msg("deleted failed-validation Velero Backup; retrying after storage location recovery")
		return subroutines.StopWithRequeue(
			3*time.Second,
			fmt.Sprintf("retrying failed-validation Velero Backup %s/%s", current.Namespace, current.Name),
		), false, nil

	case velerov1.BackupPhaseFailed, velerov1.BackupPhasePartiallyFailed:
		log.Error().
			Str("subroutine", veleroSubroutineName).
			Str("platformBackup", backup.Name).
			Str("veleroBackup", current.Name).
			Str("phase", string(current.Status.Phase)).
			Msg("Velero Backup failed")
		return subroutines.OK(), false, fmt.Errorf(
			"velero backup %s/%s reached phase %s: %s (errors=%d)",
			current.Namespace,
			current.Name,
			current.Status.Phase,
			current.Status.FailureReason,
			current.Status.Errors,
		)

	default:
		log.Info().
			Str("subroutine", veleroSubroutineName).
			Str("platformBackup", backup.Name).
			Str("veleroBackup", current.Name).
			Str("phase", string(current.Status.Phase)).
			Msg("Velero Backup not complete yet")
		return subroutines.StopWithRequeue(
			3*time.Second, fmt.Sprintf("waiting for Velero Backup %s/%s", current.Namespace, current.Name)), false, nil
	}

	log.Info().
		Str("subroutine", veleroSubroutineName).
		Str("platformBackup", backup.Name).
		Msg("Velero backup tasks succeeded")
	return subroutines.OK(), statusChanged, nil
}
