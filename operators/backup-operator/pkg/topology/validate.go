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

package topology

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ConditionTopologyValidated is set on PlatformRestore after the KCP-shard
	// topology check passes (or is skipped when validation is not Strict).
	ConditionTopologyValidated = "TopologyValidated"
)

// ValidateSubroutine compares the KCP-shard topology recorded in the source
// PlatformBackup against the live Etcd CRs in the operator namespace.
//
// Scope (KCP shards only): the subroutine checks that every shard name in the
// backup artefact exists as a live Etcd CR carrying the kcp-shard label, and
// that no live shard is absent from the backup. Other topology dimensions
// (CNPG, OpenFGA, Velero) are not yet captured in the backup artefact and are
// therefore skipped.
//
// When TopologyValidation is not Strict the subroutine is a no-op and always
// returns OK — downstream subroutines proceed unconditionally.
type ValidateSubroutine struct {
	namespace string
}

func NewValidateSubroutine(namespace string) *ValidateSubroutine {
	return &ValidateSubroutine{namespace: namespace}
}

func (s *ValidateSubroutine) GetName() string { return ConditionTopologyValidated }

// Process implements subroutines.Subroutine. It runs before EtcdRestoreSubroutine
// and blocks the chain on mismatch when TopologyValidation == Strict.
func (s *ValidateSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	rst, ok := obj.(*pmbackupv1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), fmt.Errorf("unexpected object type %T", obj)
	}

	if rst.Spec.TopologyValidation != pmbackupv1alpha1.TopologyValidationStrict {
		// Non-strict (None or any future mode): skip validation but signal
		// explicitly that it was bypassed rather than passed.
		return subroutines.Skip("topology validation not strict, skipping"), nil
	}

	if apimeta.IsStatusConditionTrue(rst.Status.Conditions, ConditionTopologyValidated) {
		return subroutines.OK(), nil
	}

	cl := subroutines.MustClientFromContext(ctx)
	log := logger.LoadLoggerFromContext(ctx)

	var bkp pmbackupv1alpha1.PlatformBackup
	if err := cl.Get(ctx, types.NamespacedName{Name: rst.Spec.Source.BackupID}, &bkp); err != nil {
		if apierrors.IsNotFound(err) {
			return subroutines.StopWithRequeue(30*time.Second,
				fmt.Sprintf("source backup %q not found, requeueing", rst.Spec.Source.BackupID)), nil
		}
		return subroutines.OK(), fmt.Errorf("fetching source backup %q: %w", rst.Spec.Source.BackupID, err)
	}

	if bkp.Status.Artefacts.Etcd == nil || len(bkp.Status.Artefacts.Etcd.Shards) == 0 {
		// Backup captured no etcd shards — nothing to compare against.
		// Use Skip rather than OK so the condition reason is "Skipped" not
		// "Complete", making it visible that validation was not performed.
		log.Debug().Str("restore", rst.Name).Msg("topology validation skipped: no etcd artefacts in source backup")
		return subroutines.Skip("no etcd artefacts in source backup"), nil
	}

	var list druidv1alpha1.EtcdList
	if err := cl.List(ctx, &list,
		ctrlruntimeclient.InNamespace(s.namespace),
		ctrlruntimeclient.MatchingLabels{backup.LabelKeyComponent: backup.LabelComponentKCPShard},
	); err != nil {
		return subroutines.OK(), fmt.Errorf("listing live Etcd CRs: %w", err)
	}

	liveShards := make(map[string]struct{}, len(list.Items))
	for _, e := range list.Items {
		liveShards[e.Name] = struct{}{}
	}
	backupShards := bkp.Status.Artefacts.Etcd.Shards

	var mismatches []string

	for name := range backupShards {
		if _, ok := liveShards[name]; !ok {
			mismatches = append(mismatches, fmt.Sprintf("shard %q present in backup but missing from live cluster", name))
		}
	}

	for name := range liveShards {
		if _, ok := backupShards[name]; !ok {
			mismatches = append(mismatches, fmt.Sprintf("shard %q present in live cluster but absent from backup", name))
		}
	}

	if len(mismatches) > 0 {
		sort.Strings(mismatches) // deterministic condition message across reconciles
		msg := fmt.Sprintf("KCP shard topology mismatch: %s", strings.Join(mismatches, "; "))
		log.Warn().Str("restore", rst.Name).Str("backup", rst.Spec.Source.BackupID).Msg(msg)
		return subroutines.StopWithRequeue(5*time.Second, msg), nil
	}

	log.Info().
		Str("restore", rst.Name).
		Str("backup", rst.Spec.Source.BackupID).
		Int("shardCount", len(backupShards)).
		Msg("KCP shard topology validated")
	return subroutines.OK(), nil
}
