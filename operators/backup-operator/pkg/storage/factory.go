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

package storage

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Factory opens the stores used by a backup.
type Factory interface {
	// ForBackupStorage opens the bucket of a BackupStorage.
	ForBackupStorage(ctx context.Context, cl ctrlruntimeclient.Client, bs *pmbackupv1alpha1.BackupStorage) (Store, error)
	// ForEtcd opens the druid snapshot store of an Etcd and returns the key prefix of its snapshots.
	ForEtcd(ctx context.Context, cl ctrlruntimeclient.Client, etcd *druidv1alpha1.Etcd) (Store, string, error)
}

// S3Factory opens real S3 buckets.
type S3Factory struct {
	// EtcdEndpoint replaces the endpoint of the druid stores, for a CLI outside the cluster.
	EtcdEndpoint string
}

func (S3Factory) ForBackupStorage(ctx context.Context, cl ctrlruntimeclient.Client, bs *pmbackupv1alpha1.BackupStorage) (Store, error) {
	secret := &corev1.Secret{}
	if err := cl.Get(ctx, types.NamespacedName{Namespace: bs.Namespace, Name: bs.Spec.CredentialsSecretRef.Name}, secret); err != nil {
		return nil, fmt.Errorf("getting credentials secret: %w", err)
	}
	return NewS3(S3Config{
		Endpoint:        bs.Spec.Endpoint,
		Region:          bs.Spec.Region,
		Bucket:          bs.Spec.Bucket,
		AccessKeyID:     string(secret.Data["accessKeyID"]),
		SecretAccessKey: string(secret.Data["secretAccessKey"]),
		ForcePathStyle:  bs.Spec.ForcePathStyle,
	})
}

func (f S3Factory) ForEtcd(ctx context.Context, cl ctrlruntimeclient.Client, etcd *druidv1alpha1.Etcd) (Store, string, error) {
	store := etcd.Spec.Backup.Store
	if err := CheckEtcdStore(etcd); err != nil {
		return nil, "", err
	}
	ns := store.SecretRef.Namespace
	if ns == "" {
		ns = etcd.Namespace
	}
	secret := &corev1.Secret{}
	if err := cl.Get(ctx, types.NamespacedName{Namespace: ns, Name: store.SecretRef.Name}, secret); err != nil {
		return nil, "", fmt.Errorf("getting druid store secret: %w", err)
	}
	endpoint := string(secret.Data["endpoint"])
	if store.EndpointOverride != nil && *store.EndpointOverride != "" {
		endpoint = *store.EndpointOverride
	}
	if f.EtcdEndpoint != "" {
		endpoint = f.EtcdEndpoint
	}
	pathStyle, _ := strconv.ParseBool(string(secret.Data["s3ForcePathStyle"]))
	s, err := NewS3(S3Config{
		Endpoint:        endpoint,
		Region:          string(secret.Data["region"]),
		Bucket:          *store.Container,
		AccessKeyID:     string(secret.Data["accessKeyID"]),
		SecretAccessKey: string(secret.Data["secretAccessKey"]),
		ForcePathStyle:  pathStyle,
	})
	return s, store.Prefix, err
}

// Opened is where a backup archive is written, with its encryption key.
type Opened struct {
	Store  Store
	Prefix string
	Key    []byte
}

// OpenBackupStorage gets the BackupStorage, its encryption key and opens the bucket.
func OpenBackupStorage(ctx context.Context, cl ctrlruntimeclient.Client, f Factory, namespace, name string) (*Opened, error) {
	bs := &pmbackupv1alpha1.BackupStorage{}
	if err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, bs); err != nil {
		return nil, fmt.Errorf("getting backupstorage %s: %w", name, err)
	}
	keySecret := &corev1.Secret{}
	if err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: bs.Spec.EncryptionKeySecretRef.Name}, keySecret); err != nil {
		return nil, fmt.Errorf("getting encryption key secret: %w", err)
	}
	key := keySecret.Data["key"]
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key secret %s: key must be 32 bytes, got %d", keySecret.Name, len(key))
	}
	s, err := f.ForBackupStorage(ctx, cl, bs)
	if err != nil {
		return nil, err
	}
	return &Opened{Store: s, Prefix: bs.Spec.Prefix, Key: key}, nil
}

// CheckEtcdStore returns an error when the Etcd has no S3 snapshot store.
func CheckEtcdStore(etcd *druidv1alpha1.Etcd) error {
	store := etcd.Spec.Backup.Store
	switch {
	case store == nil:
		return fmt.Errorf("etcd %s has no backup store", etcd.Name)
	case store.Provider == nil || *store.Provider != "S3":
		return fmt.Errorf("etcd %s: only the S3 store provider is supported", etcd.Name)
	case store.Container == nil || *store.Container == "" || store.SecretRef == nil:
		return fmt.Errorf("etcd %s: store needs container and secretRef", etcd.Name)
	}
	return nil
}

// CheckEtcdStorePrefixes returns an error when the store prefix of one Etcd starts with the prefix
// of another one in the same bucket. druid lists without a trailing slash and mixes their snapshots.
func CheckEtcdStorePrefixes(etcds []druidv1alpha1.Etcd) error {
	for i := range etcds {
		for j := range etcds {
			a, b := etcds[i].Spec.Backup.Store, etcds[j].Spec.Backup.Store
			if i == j || a == nil || b == nil || a.Container == nil || b.Container == nil || *a.Container != *b.Container {
				continue
			}
			if strings.HasPrefix(a.Prefix, b.Prefix) {
				return fmt.Errorf("etcd %s: store prefix %q starts with %q of etcd %s in the same bucket, druid would mix their snapshots",
					etcds[i].Name, a.Prefix, b.Prefix, etcds[j].Name)
			}
		}
	}
	return nil
}

// MemoryFactory returns in-memory stores, one per bucket. For tests.
type MemoryFactory struct {
	mu      sync.Mutex
	Buckets map[string]*Memory
}

func (f *MemoryFactory) Bucket(name string) *Memory {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Buckets == nil {
		f.Buckets = map[string]*Memory{}
	}
	if f.Buckets[name] == nil {
		f.Buckets[name] = NewMemory()
	}
	return f.Buckets[name]
}

func (f *MemoryFactory) ForBackupStorage(_ context.Context, _ ctrlruntimeclient.Client, bs *pmbackupv1alpha1.BackupStorage) (Store, error) {
	return f.Bucket(bs.Spec.Bucket), nil
}

func (f *MemoryFactory) ForEtcd(_ context.Context, _ ctrlruntimeclient.Client, etcd *druidv1alpha1.Etcd) (Store, string, error) {
	if err := CheckEtcdStore(etcd); err != nil {
		return nil, "", err
	}
	return f.Bucket(*etcd.Spec.Backup.Store.Container), etcd.Spec.Backup.Store.Prefix, nil
}
