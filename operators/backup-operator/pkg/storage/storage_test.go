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

package storage_test

import (
	"context"
	"errors"
	"testing"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/backup-operator/pkg/manifest"
	"go.platform-mesh.io/backup-operator/pkg/storage"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestCopyBetweenStores(t *testing.T) {
	ctx := context.Background()
	src, dst := storage.NewMemory(), storage.NewMemory()
	require.NoError(t, storage.PutBytes(ctx, src, "a/snap", []byte("data")))

	sum, err := storage.Copy(ctx, src, "a/snap", dst, "b/snap", 4)
	require.NoError(t, err)
	assert.Equal(t, manifest.SHA256([]byte("data")), sum)
	got, err := storage.GetBytes(ctx, dst, "b/snap")
	require.NoError(t, err)
	assert.Equal(t, "data", string(got))

	_, err = storage.Copy(ctx, src, "missing", dst, "x", 0)
	assert.True(t, errors.Is(err, storage.ErrNotFound))
}

func TestNewS3Endpoint(t *testing.T) {
	_, err := storage.NewS3(storage.S3Config{Endpoint: "http://localstack:4566", Bucket: "b"})
	require.NoError(t, err)
	_, err = storage.NewS3(storage.S3Config{Bucket: "b"})
	require.NoError(t, err, "empty endpoint is AWS")
	_, err = storage.NewS3(storage.S3Config{Endpoint: "localstack", Bucket: "b"})
	assert.ErrorContains(t, err, "invalid endpoint")
}

func TestCheckEtcdStorePrefixes(t *testing.T) {
	etcd := func(name, bucket, prefix string) druidv1alpha1.Etcd {
		return druidv1alpha1.Etcd{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: druidv1alpha1.EtcdSpec{Backup: druidv1alpha1.BackupSpec{Store: &druidv1alpha1.StoreSpec{
				Container: ptr.To(bucket), Prefix: prefix,
			}}},
		}
	}
	assert.NoError(t, storage.CheckEtcdStorePrefixes([]druidv1alpha1.Etcd{
		etcd("etcd-kcp", "etcd", "etcd-kcp-root"), etcd("etcd-kcp-triton", "etcd", "etcd-kcp-triton"),
	}))
	assert.NoError(t, storage.CheckEtcdStorePrefixes([]druidv1alpha1.Etcd{
		etcd("etcd-kcp", "a", "etcd-kcp"), etcd("etcd-kcp-triton", "b", "etcd-kcp-triton"),
	}), "other buckets")
	err := storage.CheckEtcdStorePrefixes([]druidv1alpha1.Etcd{
		etcd("etcd-kcp", "etcd", "etcd-kcp"), etcd("etcd-kcp-triton", "etcd", "etcd-kcp-triton"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `etcd etcd-kcp-triton: store prefix "etcd-kcp-triton" starts with "etcd-kcp" of etcd etcd-kcp`)
}
