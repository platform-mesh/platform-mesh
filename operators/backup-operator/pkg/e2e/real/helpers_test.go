//go:build e2e_real

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

package e2e_real_test

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/require"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/backup-operator/pkg/restore"
	"go.platform-mesh.io/backup-operator/pkg/topology"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// suffix generates a short random string for unique resource names.
func suffix() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// newRealEtcdShard creates an Etcd CR configured for a real etcd-druid:
//   - No SuspendEtcdSpecReconcileAnnotation (etcd-druid drives real pods)
//   - StoreSpec points at minio via S3 provider
//   - SecretRef points at the s3-credentials Secret created by EnsureMinioDeployed
func newRealEtcdShard(name string) *druidv1alpha1.Etcd {
	minioEndpoint := "http://" + minioServiceName + "." + e2eNS + ".svc:" + "9000"
	s3Provider := druidv1alpha1.StorageProvider("S3")
	return &druidv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: e2eNS,
			Labels: map[string]string{
				backup.LabelKeyComponent: backup.LabelComponentKCPShard,
			},
		},
		Spec: druidv1alpha1.EtcdSpec{
			// etcd-druid requires Labels to be set on the spec.
			Labels:   map[string]string{"app": name, "instance": name, "name": "etcd"},
			Replicas: 1,
			Backup: druidv1alpha1.BackupSpec{
				// Port, DeltaSnapshotPeriod and FullSnapshotSchedule are required by
				// etcd-druid's backup-ready health check (check_backup_ready.go:93).
				// Omitting them causes a nil-pointer panic that crash-loops etcd-druid
				// and prevents status.ready from ever flipping to true.
				Port:                 ptr.To(int32(8080)),
				DeltaSnapshotPeriod:  &metav1.Duration{Duration: 5 * time.Minute},
				FullSnapshotSchedule: ptr.To("0 */24 * * *"),
				Store: &druidv1alpha1.StoreSpec{
					Prefix:           name,
					Provider:         &s3Provider,
					Container:        ptr.To(minioBucket),
					EndpointOverride: ptr.To(minioEndpoint),
					SecretRef: &corev1.SecretReference{
						Name:      minioSecretName,
						Namespace: e2eNS,
					},
				},
			},
		},
	}
}

// newPlatformBackup returns a cluster-scoped PlatformBackup with etcd enabled.
// The S3 storage spec here is for the backup-operator's own topology.json upload
// (etcd snapshots go to minio via the StoreSpec on the Etcd CR above).
func newPlatformBackup(name string) *backupv1alpha1.PlatformBackup {
	return &backupv1alpha1.PlatformBackup{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: backupv1alpha1.PlatformBackupSpec{
			Storage: backupv1alpha1.StorageSpec{
				S3: backupv1alpha1.S3StorageSpec{
					Endpoint:       "http://" + minioServiceName + "." + e2eNS + ".svc:9000",
					Bucket:         minioBucket,
					CredentialsRef: corev1.LocalObjectReference{Name: minioSecretName},
				},
			},
			Components: backupv1alpha1.ComponentsSpec{
				Etcd: backupv1alpha1.EtcdSpec{Enabled: true},
			},
		},
	}
}

// newPlatformRestore returns a cluster-scoped PlatformRestore referencing backupID.
func newPlatformRestore(name, backupID string) *backupv1alpha1.PlatformRestore {
	return &backupv1alpha1.PlatformRestore{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: backupv1alpha1.PlatformRestoreSpec{
			Source: backupv1alpha1.RestoreSourceSpec{
				BackupID: backupID,
				Storage: backupv1alpha1.StorageSpec{
					S3: backupv1alpha1.S3StorageSpec{
						Endpoint:       "http://" + minioServiceName + "." + e2eNS + ".svc:9000",
						Bucket:         minioBucket,
						CredentialsRef: corev1.LocalObjectReference{Name: minioSecretName},
					},
				},
			},
			TopologyValidation: backupv1alpha1.TopologyValidationStrict,
		},
	}
}

// stripFinalizersAndDelete removes all finalizers from an Etcd CR then deletes it.
func stripFinalizersAndDelete(t *testing.T, etcd *druidv1alpha1.Etcd) {
	t.Helper()
	ctx := context.Background()

	var current druidv1alpha1.Etcd
	if err := cl.Get(ctx, types.NamespacedName{Name: etcd.Name, Namespace: e2eNS}, &current); err != nil {
		return
	}
	if len(current.Finalizers) > 0 {
		patch := ctrlruntimeclient.MergeFrom(current.DeepCopy())
		current.Finalizers = nil
		_ = cl.Patch(ctx, &current, patch)
	}
	_ = cl.Delete(ctx, &current)
}

// runEtcdctlPod creates a one-shot Pod running registry.k8s.io/etcd (already
// cached on the node), waits for it to complete, then deletes it. The image has
// no shell, so cmd must be a direct etcdctl invocation without shell wrapping.
func runEtcdctlPod(ctx context.Context, t *testing.T, nameSuffix string, cmd []string) error {
	t.Helper()
	_, err := runEtcdctlPodOutput(ctx, t, nameSuffix, cmd)
	return err
}

func runEtcdctlPodOutput(ctx context.Context, t *testing.T, nameSuffix string, cmd []string) (string, error) {
	t.Helper()
	podName := "etcdctl-" + nameSuffix
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: e2eNS,
			Labels:    map[string]string{"app": "etcdctl-e2e"},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:    "etcdctl",
				Image:   "registry.k8s.io/etcd:3.6.6-0",
				Command: cmd,
			}},
		},
	}
	if err := cl.Create(ctx, pod); err != nil {
		return "", fmt.Errorf("creating etcdctl pod: %w", err)
	}
	t.Cleanup(func() { _ = cl.Delete(context.Background(), pod) })

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		var p corev1.Pod
		if err := cl.Get(ctx, types.NamespacedName{Name: podName, Namespace: e2eNS}, &p); err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		switch p.Status.Phase {
		case corev1.PodSucceeded:
			req := k8sClient.CoreV1().Pods(e2eNS).GetLogs(podName, &corev1.PodLogOptions{Container: "etcdctl"})
			rc, err := req.Stream(ctx)
			if err != nil {
				return "", fmt.Errorf("streaming pod logs: %w", err)
			}
			defer rc.Close()
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(rc)
			return buf.String(), nil
		case corev1.PodFailed:
			return "", fmt.Errorf("etcdctl pod %s failed", podName)
		}
		time.Sleep(2 * time.Second)
	}
	return "", fmt.Errorf("etcdctl pod %s timed out", podName)
}

// waitForShardReady polls until the named Etcd CR has Status.Ready=true.
func waitForShardReady(t *testing.T, ctx context.Context, shard *druidv1alpha1.Etcd) {
	t.Helper()
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: shard.Name, Namespace: e2eNS}, shard); err != nil {
			return false
		}
		t.Logf("[poll] shard %s ready=%v", shard.Name, shard.Status.Ready)
		return shard.Status.Ready != nil && *shard.Status.Ready
	}, 10*time.Minute, 15*time.Second, "shard %s never became ready", shard.Name)
}

// waitForBackupComplete polls until the PlatformBackup has EtcdSnapshotted=True.
func waitForBackupComplete(t *testing.T, ctx context.Context, bkp *backupv1alpha1.PlatformBackup) {
	t.Helper()
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: bkp.Name}, bkp); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] backup %s EtcdSnapshotted=%v", bkp.Name, cond)
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	}, 10*time.Minute, 15*time.Second, "backup %s EtcdSnapshotted never became True", bkp.Name)
}

// waitForRestoreComplete polls until the PlatformRestore has both
// TopologyValidated=True and EtcdRestored=True.
// TopologyValidated is checked first since it gates the restore chain; if it is
// False the restore will never progress to EtcdRestored.
func waitForRestoreComplete(t *testing.T, ctx context.Context, rst *backupv1alpha1.PlatformRestore) {
	t.Helper()
	// Wait for topology validation to pass first (fast — no cluster operations needed).
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: rst.Name}, rst); err != nil {
			t.Logf("[poll] Get restore %s error: %v", rst.Name, err)
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] restore %s TopologyValidated=%v", rst.Name, cond)
		if cond != nil && cond.Status == metav1.ConditionFalse {
			t.Logf("[poll] restore %s TopologyValidated=False — %s", rst.Name, cond.Message)
		}
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, topology.ConditionTopologyValidated)
	}, 30*time.Second, 3*time.Second, "restore %s TopologyValidated never became True", rst.Name)

	// Then wait for the actual etcd restore to complete.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: rst.Name}, rst); err != nil {
			t.Logf("[poll] Get restore %s error: %v", rst.Name, err)
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		t.Logf("[poll] restore %s EtcdRestored=%v", rst.Name, cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionEtcdRestored)
	}, 15*time.Minute, 15*time.Second, "restore %s EtcdRestored never became True", rst.Name)
}
