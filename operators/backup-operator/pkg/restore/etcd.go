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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"

	"go.platform-mesh.io/backup-operator/pkg/druid"
	"go.platform-mesh.io/backup-operator/pkg/manifest"
	"go.platform-mesh.io/backup-operator/pkg/storage"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type etcdStep int

const (
	etcdStart etcdStep = iota
	etcdDataDeleted
	etcdStorePrepared
	etcdRestored
)

// BeforePrefix is where the druid store objects from before the restore are moved to. It must
// not start with storePrefix, druid lists the store without a trailing slash.
func BeforePrefix(storePrefix, id string) string {
	return path.Join("before-restore-"+id, storePrefix)
}

// etcd restores each etcd: stop it and delete the volumes, put only the backup snapshot in the
// druid store, start it so druid restores from it. Stop first, druid uploads a delta on stop.
func (r *Restore) etcd(ctx context.Context) (bool, string, error) {
	if r.etcdSteps == nil {
		r.etcdSteps = map[string]etcdStep{}
	}
	waiting := []string{}
	for _, me := range r.x.Manifest.Etcds {
		if r.etcdSteps[me.Name] == etcdRestored {
			continue
		}
		msg, err := r.etcdStep(ctx, me)
		if err != nil {
			return false, "", fmt.Errorf("etcd %s: %w", me.Name, err)
		}
		if r.etcdSteps[me.Name] != etcdRestored {
			waiting = append(waiting, me.Name+": "+msg)
		}
	}
	if len(waiting) > 0 {
		return false, strings.Join(waiting, ", "), nil
	}
	return true, "", nil
}

func (r *Restore) etcdStep(ctx context.Context, me manifest.Etcd) (string, error) {
	etcd := &druidv1alpha1.Etcd{}
	if err := r.cl.Get(ctx, types.NamespacedName{Namespace: r.opts.Namespace, Name: me.Name}, etcd); err != nil {
		return "", err
	}

	switch r.etcdSteps[me.Name] {
	case etcdStart:
		if _, ok := etcd.Annotations[ReplicasAnnotation]; !ok {
			replicas := max(etcd.Spec.Replicas, 1)
			patch := ctrlruntimeclient.MergeFrom(etcd.DeepCopy())
			if etcd.Annotations == nil {
				etcd.Annotations = map[string]string{}
			}
			etcd.Annotations[ReplicasAnnotation] = strconv.Itoa(int(replicas))
			if err := r.cl.Patch(ctx, etcd, patch); err != nil {
				return "", err
			}
		}
		if etcd.Spec.Replicas != 0 {
			if err := druid.Scale(ctx, r.cl, etcd, 0); err != nil {
				return "", err
			}
		}
		pods, err := druid.Pods(ctx, r.cl, etcd)
		if err != nil {
			return "", err
		}
		if len(pods) > 0 {
			return "waiting for etcd pods to stop", nil
		}
		pvcs, err := druid.PVCs(ctx, r.cl, etcd)
		if err != nil {
			return "", err
		}
		for i := range pvcs {
			if pvcs[i].DeletionTimestamp == nil {
				if err := r.cl.Delete(ctx, &pvcs[i]); ctrlruntimeclient.IgnoreNotFound(err) != nil {
					return "", err
				}
			}
		}
		if len(pvcs) > 0 {
			return "waiting for etcd volumes to be deleted", nil
		}
		r.etcdSteps[me.Name] = etcdDataDeleted
		return "data deleted", nil

	case etcdDataDeleted:
		store, prefix, err := r.stores.ForEtcd(ctx, r.cl, etcd)
		if err != nil {
			return "", err
		}
		if err := moveAll(ctx, store, prefix, BeforePrefix(prefix, r.id)); err != nil {
			return "", err
		}
		if err := r.upload(ctx, store, druid.SnapshotDir(prefix)+me.Snapshot, me); err != nil {
			return "", err
		}
		r.etcdSteps[me.Name] = etcdStorePrepared
		return "druid store prepared", nil

	case etcdStorePrepared:
		v, ok := etcd.Annotations[ReplicasAnnotation]
		if ok {
			n, err := strconv.Atoi(v)
			if err != nil {
				return "", fatalError{fmt.Sprintf("bad annotation %s=%q", ReplicasAnnotation, v)}
			}
			if err := druid.Scale(ctx, r.cl, etcd, int32(n)); err != nil {
				return "", err
			}
			patch := ctrlruntimeclient.MergeFrom(etcd.DeepCopy())
			delete(etcd.Annotations, ReplicasAnnotation)
			if err := r.cl.Patch(ctx, etcd, patch); err != nil {
				return "", err
			}
			return "starting etcd", nil
		}
		pods, err := druid.Pods(ctx, r.cl, etcd)
		if err != nil {
			return "", err
		}
		if int32(len(pods)) < etcd.Spec.Replicas || !allReady(pods) || !druid.IsReady(etcd) {
			return "waiting for etcd to restore and become ready", nil
		}
		r.etcdSteps[me.Name] = etcdRestored
		r.opts.Log("  etcd %s restored from %s", me.Name, me.Snapshot)
		return "", nil
	}
	return "", nil
}

func (r *Restore) upload(ctx context.Context, store storage.Store, key string, me manifest.Etcd) error {
	src, err := r.x.Open(me.Entry())
	if err != nil {
		return err
	}
	defer src.Close() //nolint:errcheck
	h := sha256.New()
	if err := store.Put(ctx, key, io.TeeReader(src, h), me.Size); err != nil {
		return fmt.Errorf("copying snapshot to the druid store: %w", err)
	}
	if hex.EncodeToString(h.Sum(nil)) != me.SHA256 {
		_ = store.Delete(ctx, key)
		return fatalError{"snapshot does not match the manifest"}
	}
	return nil
}

// moveAll moves every object under prefix/ to dst/.
func moveAll(ctx context.Context, s storage.Store, prefix, dst string) error {
	objects, err := s.List(ctx, prefix+"/")
	if err != nil {
		return fmt.Errorf("listing druid store: %w", err)
	}
	for _, o := range objects {
		to := path.Join(dst, strings.TrimPrefix(o.Key, prefix+"/"))
		if _, err := storage.Copy(ctx, s, o.Key, s, to, o.Size); err != nil {
			return fmt.Errorf("moving %s: %w", o.Key, err)
		}
		if err := s.Delete(ctx, o.Key); err != nil {
			return fmt.Errorf("deleting %s: %w", o.Key, err)
		}
	}
	return nil
}

func allReady(pods []corev1.Pod) bool {
	for _, p := range pods {
		if p.DeletionTimestamp != nil {
			return false
		}
		ok := false
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
