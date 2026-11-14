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

package druid_test

import (
	"testing"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/backup-operator/pkg/druid"
	"go.platform-mesh.io/backup-operator/pkg/storage"

	"k8s.io/utils/ptr"
)

func TestParseSnapshot(t *testing.T) {
	s, ok := druid.ParseSnapshot(storage.Object{Key: "etcd-kcp/v2/Full-00000000-00000864-1793357902"})
	assert.True(t, ok)
	assert.True(t, s.Full)
	assert.Equal(t, int64(864), s.LastRev)
	assert.Equal(t, int64(1793357902), s.TakenAt.Unix())

	s, ok = druid.ParseSnapshot(storage.Object{Key: "p/v2/Incr-00000480-00000505-1793358002.gz"})
	assert.True(t, ok)
	assert.False(t, s.Full)

	for _, key := range []string{"p/v2/README", "p/v2/Full-x-y-z", "p/v2/Full-1-2"} {
		_, ok := druid.ParseSnapshot(storage.Object{Key: key})
		assert.False(t, ok, key)
	}
}

func TestLatestFull(t *testing.T) {
	objs := []storage.Object{
		{Key: "p/v2/Full-00000000-00000010-1000"},
		{Key: "p/v2/Full-00000000-00000020-2000"},
		{Key: "p/v2/Incr-00000021-00000030-2100"},
		{Key: "p/v2/Full-00000000-00000015-1500"},
	}
	s, ok := druid.LatestFull(objs, time.Unix(1200, 0))
	assert.True(t, ok)
	assert.Equal(t, "Full-00000000-00000020-2000", s.FileName)

	_, ok = druid.LatestFull(objs, time.Unix(2001, 0))
	assert.False(t, ok, "snapshots before the task are not used")

	s, ok = druid.LatestFull(objs, time.Unix(2000, 500))
	assert.True(t, ok, "same second as the task counts")
	assert.Equal(t, int64(20), s.LastRev)
}

func TestIsReady(t *testing.T) {
	etcd := func(ready bool, replicas, readyReplicas int32) *druidv1alpha1.Etcd {
		return &druidv1alpha1.Etcd{
			Spec:   druidv1alpha1.EtcdSpec{Replicas: replicas},
			Status: druidv1alpha1.EtcdStatus{Ready: ptr.To(ready), ReadyReplicas: readyReplicas},
		}
	}
	assert.True(t, druid.IsReady(etcd(true, 1, 1)))
	assert.False(t, druid.IsReady(etcd(false, 1, 1)))
	assert.False(t, druid.IsReady(etcd(true, 3, 2)), "not all replicas")
	assert.False(t, druid.IsReady(etcd(true, 0, 0)), "scaled down")
	assert.False(t, druid.IsReady(&druidv1alpha1.Etcd{Spec: druidv1alpha1.EtcdSpec{Replicas: 1}}), "no status")
}
