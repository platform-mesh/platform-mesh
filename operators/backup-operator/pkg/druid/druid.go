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

// Package druid has helpers for etcd-druid: snapshot names, scaling and volumes.
package druid

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"

	"go.platform-mesh.io/backup-operator/pkg/storage"

	corev1 "k8s.io/api/core/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	operationAnnotation = "gardener.cloud/operation"
	partOfLabel         = "app.kubernetes.io/part-of"
	managedByLabel      = "app.kubernetes.io/managed-by"
)

// SnapshotDir is where druid writes the snapshots of a store prefix.
func SnapshotDir(prefix string) string {
	return path.Join(prefix, "v2") + "/"
}

// Snapshot is a parsed druid snapshot name: <Full|Incr>-<startRev>-<lastRev>-<unix>[.gz].
type Snapshot struct {
	Name     string
	Full     bool
	LastRev  int64
	TakenAt  time.Time
	Object   storage.Object
	FileName string
}

func ParseSnapshot(o storage.Object) (Snapshot, bool) {
	name := path.Base(o.Key)
	parts := strings.Split(strings.TrimSuffix(name, ".gz"), "-")
	if len(parts) < 4 || (parts[0] != "Full" && parts[0] != "Incr") {
		return Snapshot{}, false
	}
	lastRev, err1 := strconv.ParseInt(parts[2], 10, 64)
	unix, err2 := strconv.ParseInt(parts[3], 10, 64)
	if err1 != nil || err2 != nil {
		return Snapshot{}, false
	}
	return Snapshot{Name: name, Full: parts[0] == "Full", LastRev: lastRev, TakenAt: time.Unix(unix, 0), Object: o, FileName: name}, true
}

// LatestFull returns the newest full snapshot taken at or after notBefore.
func LatestFull(objects []storage.Object, notBefore time.Time) (Snapshot, bool) {
	var best Snapshot
	found := false
	for _, o := range objects {
		s, ok := ParseSnapshot(o)
		if !ok || !s.Full || s.TakenAt.Before(notBefore.Truncate(time.Second)) {
			continue
		}
		if !found || s.LastRev > best.LastRev {
			best, found = s, true
		}
	}
	return best, found
}

// IsReady is true when druid reports the Etcd ready with all replicas. A scaled down Etcd is
// not ready, druid reports it ready with 0 of 0 replicas.
func IsReady(etcd *druidv1alpha1.Etcd) bool {
	return etcd.Status.Ready != nil && *etcd.Status.Ready && etcd.Spec.Replicas > 0 && etcd.Status.ReadyReplicas == etcd.Spec.Replicas
}

// Scale sets spec.replicas and asks druid to reconcile.
func Scale(ctx context.Context, cl ctrlruntimeclient.Client, etcd *druidv1alpha1.Etcd, replicas int32) error {
	patch := ctrlruntimeclient.MergeFrom(etcd.DeepCopy())
	etcd.Spec.Replicas = replicas
	if etcd.Annotations == nil {
		etcd.Annotations = map[string]string{}
	}
	etcd.Annotations[operationAnnotation] = "reconcile"
	return cl.Patch(ctx, etcd, patch)
}

// Pods returns the pods of the Etcd.
func Pods(ctx context.Context, cl ctrlruntimeclient.Client, etcd *druidv1alpha1.Etcd) ([]corev1.Pod, error) {
	list := &corev1.PodList{}
	err := cl.List(ctx, list, ctrlruntimeclient.InNamespace(etcd.Namespace),
		ctrlruntimeclient.MatchingLabels{partOfLabel: etcd.Name, managedByLabel: "etcd-druid"})
	if err != nil {
		return nil, fmt.Errorf("listing etcd pods: %w", err)
	}
	return list.Items, nil
}

// PVCs returns the data volumes of the Etcd.
func PVCs(ctx context.Context, cl ctrlruntimeclient.Client, etcd *druidv1alpha1.Etcd) ([]corev1.PersistentVolumeClaim, error) {
	list := &corev1.PersistentVolumeClaimList{}
	err := cl.List(ctx, list, ctrlruntimeclient.InNamespace(etcd.Namespace),
		ctrlruntimeclient.MatchingLabels{partOfLabel: etcd.Name, managedByLabel: "etcd-druid"})
	if err != nil {
		return nil, fmt.Errorf("listing etcd volumes: %w", err)
	}
	return list.Items, nil
}
