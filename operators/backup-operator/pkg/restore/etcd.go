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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	etcdRestoreSubroutineName = "etcd-restore"
	etcdOperandNamespace      = "platform-mesh-system"
	etcdCacheName             = "etcd-cache"

	LabelKCPShard = "platform-mesh.io/kcp-shard"

	etcdRestoreStartedCondition   = "EtcdRestoreStarted"
	etcdRestoreCompletedCondition = "EtcdRestoreCompleted"

	etcdRestoreStateConfigMapSuffix = "etcd-restore-state"

	etcdRestoreStateUIDKey       = "platformRestoreUID"
	etcdRestoreStateShardsKey    = "shards"
	etcdRestoreStateCompletedKey = "completed"

	etcdRestorePhaseCaptured  = "captured"
	etcdRestorePhaseDeleting  = "deleting"
	etcdRestorePhaseRecreated = "recreated"

	// etcd-cache is deliberately not restored from the source backup: it is
	// derived state. It must nevertheless be recreated empty after the
	// authoritative shards have been restored, otherwise cache consumers keep
	// serving entries derived from the destination's pre-restore state.
	etcdCacheRestorePhaseCaptured  = "cache-captured"
	etcdCacheRestorePhaseDeleting  = "cache-deleting"
	etcdCacheRestorePhaseRecreated = "cache-recreated"
)

type EtcdRestoreSubroutine struct {
	name             string
	operandNamespace string
	client           ctrlruntimeclient.Client
}

func NewEtcdRestoreSubroutine(cli ctrlruntimeclient.Client) *EtcdRestoreSubroutine {
	return &EtcdRestoreSubroutine{
		name:             etcdRestoreSubroutineName,
		client:           cli,
		operandNamespace: etcdOperandNamespace,
	}
}

func (e *EtcdRestoreSubroutine) GetName() string {
	return e.name
}

func (e *EtcdRestoreSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	platformRestore, ok := obj.(*pmbackupv1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected a pmbackupv1alpha1.PlatformRestore, got %T", obj)
	}

	if restoreTerminal(platformRestore) {
		return subroutines.OK(), false, nil
	}

	if etcdRestoreCompleted(platformRestore) {
		return subroutines.OK(), false, nil
	}

	log := logger.LoadLoggerFromContext(ctx)

	statusChanged := setPhase(platformRestore, pmbackupv1alpha1.RestorePhaseRestoringEtcd)

	state, initialized, err := e.ensureRestoreState(ctx, platformRestore)
	if err != nil {
		return subroutines.OK(), statusChanged, err
	}

	if initialized {
		meta.SetStatusCondition(&platformRestore.Status.Conditions, metav1.Condition{
			Type:               etcdRestoreStartedCondition,
			Status:             metav1.ConditionTrue,
			Reason:             "AutomatedEtcdRestoreStarted",
			Message:            "Captured KCP etcd manifests before automated restore",
			ObservedGeneration: platformRestore.Generation,
		})
		statusChanged = true

		log.Info().
			Str("subroutine", etcdRestoreSubroutineName).
			Str("platformRestore", platformRestore.Name).
			Msg("captured KCP etcd manifests for restore")

		return subroutines.StopWithRequeue(
			10*time.Second,
			"captured KCP etcd manifests for restore",
		), statusChanged, nil
	}

	shardNames := etcdRestoreShardNames(state)
	if len(shardNames) == 0 {
		return subroutines.OK(), statusChanged, fmt.Errorf("etcd restore state %s/%s has no shards", state.Namespace, state.Name)
	}

	if state.Data[etcdRestoreStateCompletedKey] == "true" {
		ready, err := e.allRestoredShardsReady(ctx, shardNames)
		if err != nil {
			return subroutines.OK(), statusChanged, err
		}

		cacheReady, err := e.restoredEtcdReady(ctx, etcdCacheName)
		if err != nil {
			return subroutines.OK(), statusChanged, err
		}
		if !ready || !cacheReady {
			return subroutines.StopWithRequeue(
				10*time.Second,
				"waiting for completed KCP etcd and cache restore to become ready again",
			), statusChanged, nil
		}

		return subroutines.OK(), statusChanged, nil
	}

	for _, shardName := range shardNames {
		phase := state.Data[etcdRestorePhaseKey(shardName)]

		switch phase {
		case "", etcdRestorePhaseCaptured:
			return e.deleteEtcdCR(ctx, platformRestore, state, shardName, statusChanged)

		case etcdRestorePhaseDeleting:
			return e.recreateEtcdCRWhenDeleted(ctx, platformRestore, state, shardName, statusChanged)

		case etcdRestorePhaseRecreated:
			ready, err := e.restoredEtcdReady(ctx, shardName)
			if err != nil {
				return subroutines.OK(), statusChanged, err
			}

			if !ready {
				log.Info().
					Str("subroutine", etcdRestoreSubroutineName).
					Str("platformRestore", platformRestore.Name).
					Str("etcd", shardName).
					Msg("waiting for recreated kcp-etcd shard to become ready")

				return subroutines.StopWithRequeue(
					10*time.Second,
					fmt.Sprintf("waiting for recreated kcp-etcd shard %s/%s", e.operandNamespace, shardName),
				), statusChanged, nil
			}

		default:
			return subroutines.OK(), statusChanged, fmt.Errorf("unknown etcd restore phase %q for shard %s", phase, shardName)
		}
	}

	cacheReady, result, err := e.resetEtcdCache(ctx, platformRestore, state)
	if err != nil {
		return subroutines.OK(), statusChanged, err
	}
	if !cacheReady {
		return result, statusChanged, nil
	}

	stateCopy := state.DeepCopy()
	state.Data[etcdRestoreStateCompletedKey] = "true"
	if err := e.client.Patch(ctx, state, ctrlruntimeclient.MergeFrom(stateCopy)); err != nil {
		return subroutines.OK(), statusChanged, fmt.Errorf("failed to mark etcd restore state completed: %w", err)
	}

	if markPhaseReady(
		platformRestore,
		etcdRestoreCompletedCondition,
		"AutomatedEtcdRestoreCompleted",
		"All authoritative KCP etcd shards were recreated; etcd-cache was recreated empty and is ready",
	) {
		statusChanged = true
	}

	log.Info().
		Str("subroutine", etcdRestoreSubroutineName).
		Str("platformRestore", platformRestore.Name).
		Int("count", len(shardNames)).
		Msg("all authoritative kcp-etcd shards are restored and etcd-cache was recreated empty")

	return subroutines.OK(), statusChanged, nil
}

func (e *EtcdRestoreSubroutine) ensureRestoreState(
	ctx context.Context,
	platformRestore *pmbackupv1alpha1.PlatformRestore,
) (*corev1.ConfigMap, bool, error) {
	name := etcdRestoreStateConfigMapName(platformRestore)

	state := &corev1.ConfigMap{}
	err := e.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Namespace: e.operandNamespace,
		Name:      name,
	}, state)

	if err == nil {
		if state.Data[etcdRestoreStateUIDKey] == string(platformRestore.UID) {
			return state, false, nil
		}

		if deleteErr := e.client.Delete(ctx, state); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			return nil, false, fmt.Errorf("failed to delete stale etcd restore state %s/%s: %w", state.Namespace, state.Name, deleteErr)
		}

		return nil, false, fmt.Errorf("deleted stale etcd restore state %s/%s; retry restore reconciliation", state.Namespace, state.Name)
	}

	if !apierrors.IsNotFound(err) {
		return nil, false, fmt.Errorf("failed to get etcd restore state %s/%s: %w", e.operandNamespace, name, err)
	}

	shards, err := e.listAllKCPShards(ctx)
	if err != nil {
		return nil, false, err
	}

	if len(shards.Items) == 0 {
		return nil, false, fmt.Errorf(
			"no kcp-etcd shards found in namespace %s with label %s=true",
			e.operandNamespace,
			LabelKCPShard,
		)
	}

	data := map[string]string{
		etcdRestoreStateUIDKey: string(platformRestore.UID),
	}

	shardNames := make([]string, 0, len(shards.Items))
	cacheCaptured := false

	for i := range shards.Items {
		shard := &shards.Items[i]

		manifest, err := sanitizedEtcdManifest(shard)
		if err != nil {
			return nil, false, fmt.Errorf("failed to sanitize Etcd %s/%s manifest: %w", shard.GetNamespace(), shard.GetName(), err)
		}

		data[etcdRestoreManifestKey(shard.GetName())] = manifest
		if shard.GetName() == etcdCacheName {
			data[etcdRestorePhaseKey(shard.GetName())] = etcdCacheRestorePhaseCaptured
			cacheCaptured = true
			continue
		}
		shardNames = append(shardNames, shard.GetName())
		data[etcdRestorePhaseKey(shard.GetName())] = etcdRestorePhaseCaptured
	}
	if !cacheCaptured {
		return nil, false, fmt.Errorf("no %s Etcd found; cannot reset derived KCP cache", etcdCacheName)
	}

	data[etcdRestoreStateShardsKey] = strings.Join(shardNames, ",")

	state = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: e.operandNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":            "platform-mesh-backup-operator",
				"app.kubernetes.io/part-of":               "platform-mesh",
				"backup.platform-mesh.io/platformrestore": platformRestore.Name,
			},
		},
		Data: data,
	}

	if err := e.client.Create(ctx, state); err != nil {
		return nil, false, fmt.Errorf("failed to create etcd restore state %s/%s: %w", state.Namespace, state.Name, err)
	}

	return state, true, nil
}

func (e *EtcdRestoreSubroutine) deleteEtcdCR(
	ctx context.Context,
	platformRestore *pmbackupv1alpha1.PlatformRestore,
	state *corev1.ConfigMap,
	shardName string,
	statusChanged bool,
) (subroutines.Result, bool, error) {
	log := logger.LoadLoggerFromContext(ctx)

	etcd := etcdObject(shardName)

	err := e.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Namespace: e.operandNamespace,
		Name:      shardName,
	}, etcd)

	if err != nil && !apierrors.IsNotFound(err) {
		return subroutines.OK(), statusChanged, fmt.Errorf("failed to get Etcd %s/%s before deletion: %w", e.operandNamespace, shardName, err)
	}

	if err == nil {
		if deleteErr := e.client.Delete(ctx, etcd); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			return subroutines.OK(), statusChanged, fmt.Errorf("failed to delete Etcd %s/%s: %w", e.operandNamespace, shardName, deleteErr)
		}
	}

	if err := e.deleteEtcdPodsAndPVCs(ctx, shardName); err != nil {
		return subroutines.OK(), statusChanged, err
	}

	if err := e.setShardPhase(ctx, state, shardName, etcdRestorePhaseDeleting); err != nil {
		return subroutines.OK(), statusChanged, err
	}

	log.Info().
		Str("subroutine", etcdRestoreSubroutineName).
		Str("platformRestore", platformRestore.Name).
		Str("etcd", shardName).
		Msg("deleted Etcd CR and runtime objects for restore")

	return subroutines.StopWithRequeue(
		10*time.Second,
		fmt.Sprintf("waiting for Etcd %s/%s to be deleted", e.operandNamespace, shardName),
	), statusChanged, nil
}

func (e *EtcdRestoreSubroutine) recreateEtcdCRWhenDeleted(
	ctx context.Context,
	platformRestore *pmbackupv1alpha1.PlatformRestore,
	state *corev1.ConfigMap,
	shardName string,
	statusChanged bool,
) (subroutines.Result, bool, error) {
	log := logger.LoadLoggerFromContext(ctx)

	current := etcdObject(shardName)

	err := e.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Namespace: e.operandNamespace,
		Name:      shardName,
	}, current)

	if err == nil {
		if err := e.deleteEtcdPodsAndPVCs(ctx, shardName); err != nil {
			return subroutines.OK(), statusChanged, err
		}

		log.Info().
			Str("subroutine", etcdRestoreSubroutineName).
			Str("platformRestore", platformRestore.Name).
			Str("etcd", shardName).
			Msg("waiting for Etcd CR to finish deleting")

		return subroutines.StopWithRequeue(
			10*time.Second,
			fmt.Sprintf("waiting for Etcd %s/%s to finish deleting", e.operandNamespace, shardName),
		), statusChanged, nil
	}

	if !apierrors.IsNotFound(err) {
		return subroutines.OK(), statusChanged, fmt.Errorf("failed to get Etcd %s/%s while waiting for deletion: %w", e.operandNamespace, shardName, err)
	}

	if err := e.deleteEtcdPodsAndPVCs(ctx, shardName); err != nil {
		return subroutines.OK(), statusChanged, err
	}

	restored, err := etcdFromRestoreState(state, shardName)
	if err != nil {
		return subroutines.OK(), statusChanged, err
	}

	if err := e.client.Create(ctx, restored); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return subroutines.OK(), statusChanged, fmt.Errorf("failed to recreate Etcd %s/%s: %w", e.operandNamespace, shardName, err)
		}
	}

	if err := e.setShardPhase(ctx, state, shardName, etcdRestorePhaseRecreated); err != nil {
		return subroutines.OK(), statusChanged, err
	}

	log.Info().
		Str("subroutine", etcdRestoreSubroutineName).
		Str("platformRestore", platformRestore.Name).
		Str("etcd", shardName).
		Msg("recreated Etcd CR from captured manifest")

	return subroutines.StopWithRequeue(
		10*time.Second,
		fmt.Sprintf("waiting for recreated Etcd %s/%s", e.operandNamespace, shardName),
	), statusChanged, nil
}

func (e *EtcdRestoreSubroutine) restoredEtcdReady(ctx context.Context, shardName string) (bool, error) {
	etcd := etcdObject(shardName)

	err := e.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Namespace: e.operandNamespace,
		Name:      shardName,
	}, etcd)

	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get recreated Etcd %s/%s: %w", e.operandNamespace, shardName, err)
	}

	return etcdReady(etcd), nil
}

func (e *EtcdRestoreSubroutine) allRestoredShardsReady(ctx context.Context, shardNames []string) (bool, error) {
	for _, shardName := range shardNames {
		ready, err := e.restoredEtcdReady(ctx, shardName)
		if err != nil {
			return false, err
		}
		if !ready {
			return false, nil
		}
	}

	return true, nil
}

func (e *EtcdRestoreSubroutine) listAllKCPShards(ctx context.Context) (*unstructured.UnstructuredList, error) {
	shards := &unstructured.UnstructuredList{}
	shards.SetAPIVersion("druid.gardener.cloud/v1alpha1")
	shards.SetKind("EtcdList")

	if err := e.client.List(ctx, shards,
		ctrlruntimeclient.InNamespace(e.operandNamespace),
		ctrlruntimeclient.MatchingLabels{LabelKCPShard: "true"},
	); err != nil {
		return nil, fmt.Errorf("failed to list kcp-etcd shards: %w", err)
	}
	return shards, nil
}

// resetEtcdCache recreates the destination cache Etcd from its destination
// manifest only. No source snapshot is used: this causes cache-server to
// rebuild derived entries from the restored authoritative KCP shards.
func (e *EtcdRestoreSubroutine) resetEtcdCache(
	ctx context.Context,
	platformRestore *pmbackupv1alpha1.PlatformRestore,
	state *corev1.ConfigMap,
) (bool, subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)

	switch state.Data[etcdRestorePhaseKey(etcdCacheName)] {
	case etcdCacheRestorePhaseCaptured:
		cache := etcdObject(etcdCacheName)
		err := e.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: e.operandNamespace, Name: etcdCacheName}, cache)
		if err != nil && !apierrors.IsNotFound(err) {
			return false, subroutines.OK(), fmt.Errorf("get etcd-cache before reset: %w", err)
		}
		if err == nil {
			if err := e.client.Delete(ctx, cache); err != nil && !apierrors.IsNotFound(err) {
				return false, subroutines.OK(), fmt.Errorf("delete etcd-cache before reset: %w", err)
			}
		}
		if err := e.deleteEtcdPodsAndPVCs(ctx, etcdCacheName); err != nil {
			return false, subroutines.OK(), err
		}
		if err := e.setShardPhase(ctx, state, etcdCacheName, etcdCacheRestorePhaseDeleting); err != nil {
			return false, subroutines.OK(), err
		}
		log.Info().
			Str("subroutine", etcdRestoreSubroutineName).
			Str("platformRestore", platformRestore.Name).
			Str("etcd", etcdCacheName).
			Msg("deleted destination etcd-cache so derived KCP state can rebuild")
		return false, subroutines.StopWithRequeue(10*time.Second, "waiting for etcd-cache to be deleted"), nil
	case etcdCacheRestorePhaseDeleting:
		current := etcdObject(etcdCacheName)
		err := e.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: e.operandNamespace, Name: etcdCacheName}, current)
		if err == nil {
			if err := e.deleteEtcdPodsAndPVCs(ctx, etcdCacheName); err != nil {
				return false, subroutines.OK(), err
			}
			return false, subroutines.StopWithRequeue(10*time.Second, "waiting for etcd-cache to finish deleting"), nil
		}
		if !apierrors.IsNotFound(err) {
			return false, subroutines.OK(), fmt.Errorf("get etcd-cache while waiting for deletion: %w", err)
		}
		if err := e.deleteEtcdPodsAndPVCs(ctx, etcdCacheName); err != nil {
			return false, subroutines.OK(), err
		}
		cache, err := etcdFromRestoreState(state, etcdCacheName)
		if err != nil {
			return false, subroutines.OK(), err
		}
		if err := e.client.Create(ctx, cache); err != nil && !apierrors.IsAlreadyExists(err) {
			return false, subroutines.OK(), fmt.Errorf("recreate empty etcd-cache: %w", err)
		}
		if err := e.setShardPhase(ctx, state, etcdCacheName, etcdCacheRestorePhaseRecreated); err != nil {
			return false, subroutines.OK(), err
		}
		return false, subroutines.StopWithRequeue(10*time.Second, "waiting for recreated empty etcd-cache"), nil
	case etcdCacheRestorePhaseRecreated:
		ready, err := e.restoredEtcdReady(ctx, etcdCacheName)
		if err != nil {
			return false, subroutines.OK(), err
		}
		if !ready {
			return false, subroutines.StopWithRequeue(10*time.Second, "waiting for recreated empty etcd-cache to become ready"), nil
		}
		return true, subroutines.OK(), nil
	default:
		return false, subroutines.OK(), fmt.Errorf("unknown etcd-cache restore phase %q", state.Data[etcdRestorePhaseKey(etcdCacheName)])
	}
}

func (e *EtcdRestoreSubroutine) deleteEtcdPodsAndPVCs(ctx context.Context, shardName string) error {
	grace := int64(0)

	pods := &corev1.PodList{}
	if err := e.client.List(ctx, pods, ctrlruntimeclient.InNamespace(e.operandNamespace)); err != nil {
		return fmt.Errorf("failed to list pods in namespace %s: %w", e.operandNamespace, err)
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if !strings.Contains(pod.Name, shardName) {
			continue
		}

		if err := e.client.Delete(ctx, pod, &ctrlruntimeclient.DeleteOptions{
			GracePeriodSeconds: &grace,
		}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := e.client.List(ctx, pvcs, ctrlruntimeclient.InNamespace(e.operandNamespace)); err != nil {
		return fmt.Errorf("failed to list PVCs in namespace %s: %w", e.operandNamespace, err)
	}

	for i := range pvcs.Items {
		pvc := &pvcs.Items[i]
		if !strings.Contains(pvc.Name, shardName) {
			continue
		}

		if err := e.client.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete PVC %s/%s: %w", pvc.Namespace, pvc.Name, err)
		}
	}

	return nil
}

func (e *EtcdRestoreSubroutine) setShardPhase(
	ctx context.Context,
	state *corev1.ConfigMap,
	shardName string,
	phase string,
) error {
	original := state.DeepCopy()

	if state.Data == nil {
		state.Data = map[string]string{}
	}

	state.Data[etcdRestorePhaseKey(shardName)] = phase

	if err := e.client.Patch(ctx, state, ctrlruntimeclient.MergeFrom(original)); err != nil {
		return fmt.Errorf("failed to update etcd restore state %s/%s for shard %s: %w", state.Namespace, state.Name, shardName, err)
	}

	return nil
}

func etcdObject(name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("druid.gardener.cloud/v1alpha1")
	obj.SetKind("Etcd")
	obj.SetNamespace(etcdOperandNamespace)
	obj.SetName(name)
	return obj
}

func sanitizedEtcdManifest(etcd *unstructured.Unstructured) (string, error) {
	etcdCopy := etcd.DeepCopy()

	unstructured.RemoveNestedField(etcdCopy.Object, "status")

	metadata, ok := etcdCopy.Object["metadata"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("etcd %s/%s has invalid metadata", etcd.GetNamespace(), etcd.GetName())
	}

	delete(metadata, "uid")
	delete(metadata, "resourceVersion")
	delete(metadata, "generation")
	delete(metadata, "creationTimestamp")
	delete(metadata, "managedFields")
	delete(metadata, "finalizers")
	delete(metadata, "deletionTimestamp")
	delete(metadata, "deletionGracePeriodSeconds")

	raw, err := json.Marshal(etcdCopy.Object)
	if err != nil {
		return "", err
	}

	return string(raw), nil
}

func etcdFromRestoreState(state *corev1.ConfigMap, shardName string) (*unstructured.Unstructured, error) {
	raw := state.Data[etcdRestoreManifestKey(shardName)]
	if raw == "" {
		return nil, fmt.Errorf("missing captured Etcd manifest for shard %s in restore state %s/%s", shardName, state.Namespace, state.Name)
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal captured Etcd manifest for shard %s: %w", shardName, err)
	}

	return &unstructured.Unstructured{Object: obj}, nil
}

func etcdRestoreShardNames(state *corev1.ConfigMap) []string {
	raw := strings.TrimSpace(state.Data[etcdRestoreStateShardsKey])
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	names := make([]string, 0, len(parts))

	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "" && name != etcdCacheName {
			names = append(names, name)
		}
	}

	return names
}

func etcdRestoreStateConfigMapName(platformRestore *pmbackupv1alpha1.PlatformRestore) string {
	return fmt.Sprintf("%s-%s", platformRestore.Name, etcdRestoreStateConfigMapSuffix)
}

func etcdRestoreManifestKey(shardName string) string {
	return fmt.Sprintf("%s.manifest", shardName)
}

func etcdRestorePhaseKey(shardName string) string {
	return fmt.Sprintf("%s.phase", shardName)
}

func etcdReady(etcd *unstructured.Unstructured) bool {
	ready, found, err := unstructured.NestedBool(etcd.Object, "status", "ready")
	return err == nil && found && ready
}

func etcdRestoreCompleted(platformRestore *pmbackupv1alpha1.PlatformRestore) bool {
	condition := meta.FindStatusCondition(
		platformRestore.Status.Conditions,
		etcdRestoreCompletedCondition,
	)

	return condition != nil &&
		condition.Status == metav1.ConditionTrue &&
		condition.ObservedGeneration == platformRestore.Generation
}
