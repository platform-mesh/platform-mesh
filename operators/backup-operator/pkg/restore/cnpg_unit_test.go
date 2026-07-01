//go:build !integration

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

package restore_test

import (
	"context"
	"fmt"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/restore"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const cnpgClusterNS = "cnpg-system"

func newCNPGRestoreScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, pmbackupv1alpha1.AddToScheme(s))
	require.NoError(t, cnpgv1.AddToScheme(s))
	return s
}

func fakePlatformBackupWithCNPG(name string, backups map[string]string) *pmbackupv1alpha1.PlatformBackup {
	b := &pmbackupv1alpha1.PlatformBackup{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: pmbackupv1alpha1.PlatformBackupSpec{
			Storage: pmbackupv1alpha1.StorageSpec{
				S3: pmbackupv1alpha1.S3StorageSpec{
					Endpoint:       "http://minio:9000",
					Bucket:         "backups",
					CredentialsRef: corev1.LocalObjectReference{Name: "s3-credentials"},
				},
			},
			Components: pmbackupv1alpha1.ComponentsSpec{
				CNPG: pmbackupv1alpha1.CNPGSpec{Enabled: true},
			},
		},
	}
	if backups != nil {
		b.Status.Artefacts.CNPG = &pmbackupv1alpha1.CNPGArtefact{Backups: backups}
	}
	return b
}

func fakeCNPGCluster(name string, readyInstances, totalInstances int, annotation string) *cnpgv1.Cluster {
	c := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cnpgClusterNS,
		},
		Spec: cnpgv1.ClusterSpec{
			Instances: totalInstances,
		},
	}
	c.Status.ReadyInstances = readyInstances
	if annotation != "" {
		c.Annotations = map[string]string{
			"backup.platform-mesh.io/restored-from-cnpg-backup": annotation,
		}
	}
	return c
}

func fakePlatformRestoreCNPG(name, backupID string) *pmbackupv1alpha1.PlatformRestore {
	return &pmbackupv1alpha1.PlatformRestore{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: pmbackupv1alpha1.PlatformRestoreSpec{
			Source: pmbackupv1alpha1.RestoreSourceSpec{
				BackupID: backupID,
				Storage: pmbackupv1alpha1.StorageSpec{
					S3: pmbackupv1alpha1.S3StorageSpec{
						Endpoint:       "http://minio:9000",
						Bucket:         "backups",
						CredentialsRef: corev1.LocalObjectReference{Name: "s3-credentials"},
					},
				},
			},
		},
	}
}

func newCNPGRestoreSub() *restore.CNPGRestoreSubroutine {
	return restore.NewCNPGRestoreSubroutine(unitTestNamespace, cnpgClusterNS)
}

// TestCNPGRestore_WrongObjectType verifies an error is returned for non-PlatformRestore objects.
func TestCNPGRestore_WrongObjectType(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newCNPGRestoreScheme(t)).Build()
	bkp := &pmbackupv1alpha1.PlatformBackup{ObjectMeta: metav1.ObjectMeta{Name: "b"}}
	_, err := newCNPGRestoreSub().Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected object type")
}

// TestCNPGRestore_BackupNotFound verifies StopWithRequeue when the source backup is missing.
func TestCNPGRestore_BackupNotFound(t *testing.T) {
	rst := fakePlatformRestoreCNPG("r", "nonexistent")
	cl := fake.NewClientBuilder().WithScheme(newCNPGRestoreScheme(t)).WithObjects(rst).Build()

	result, err := newCNPGRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue())
}

// TestCNPGRestore_NoCNPGArtefacts verifies OK is returned when backup has no CNPG artefacts.
func TestCNPGRestore_NoCNPGArtefacts(t *testing.T) {
	bkp := fakePlatformBackupWithCNPG("bkp", nil)
	rst := fakePlatformRestoreCNPG("r", "bkp")
	cl := fake.NewClientBuilder().WithScheme(newCNPGRestoreScheme(t)).WithObjects(bkp, rst).Build()

	result, err := newCNPGRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestCNPGRestore_Idempotent verifies re-reconcile is a no-op when CNPGRestored=True.
func TestCNPGRestore_Idempotent(t *testing.T) {
	bkp := fakePlatformBackupWithCNPG("bkp", map[string]string{"openfga-db": "bkp-openfga-db"})
	rst := fakePlatformRestoreCNPG("r", "bkp")
	rst.Status.Conditions = []metav1.Condition{{
		Type:               restore.ConditionCNPGRestored,
		Status:             metav1.ConditionTrue,
		Reason:             "Complete",
		LastTransitionTime: metav1.Now(),
	}}
	cl := fake.NewClientBuilder().WithScheme(newCNPGRestoreScheme(t)).WithObjects(bkp).Build()

	result, err := newCNPGRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestCNPGRestore_SingleCluster_AnnotationAndReady verifies OK when cluster has annotation and is ready.
func TestCNPGRestore_SingleCluster_AnnotationAndReady(t *testing.T) {
	bkp := fakePlatformBackupWithCNPG("bkp", map[string]string{"openfga-db": "bkp-openfga-db"})
	rst := fakePlatformRestoreCNPG("r", "bkp")
	cluster := fakeCNPGCluster("openfga-db", 1, 1, "bkp-openfga-db")

	cl := fake.NewClientBuilder().WithScheme(newCNPGRestoreScheme(t)).
		WithObjects(bkp, rst, cluster).WithStatusSubresource(&cnpgv1.Cluster{}).Build()

	result, err := newCNPGRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue(), "expected OK when all clusters restored and ready")
}

// TestCNPGRestore_SingleCluster_AnnotationNotReady verifies Pending when annotation set but instances not ready.
func TestCNPGRestore_SingleCluster_AnnotationNotReady(t *testing.T) {
	bkp := fakePlatformBackupWithCNPG("bkp", map[string]string{"openfga-db": "bkp-openfga-db"})
	rst := fakePlatformRestoreCNPG("r", "bkp")
	cluster := fakeCNPGCluster("openfga-db", 0, 1, "bkp-openfga-db") // 0/1 ready

	cl := fake.NewClientBuilder().WithScheme(newCNPGRestoreScheme(t)).
		WithObjects(bkp, rst, cluster).WithStatusSubresource(&cnpgv1.Cluster{}).Build()

	result, err := newCNPGRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending())
}

// TestCNPGRestore_SingleCluster_DeleteAndRecreate verifies delete+recreate is triggered for untouched cluster.
func TestCNPGRestore_SingleCluster_DeleteAndRecreate(t *testing.T) {
	bkp := fakePlatformBackupWithCNPG("bkp", map[string]string{"openfga-db": "bkp-openfga-db"})
	rst := fakePlatformRestoreCNPG("r", "bkp")
	cluster := fakeCNPGCluster("openfga-db", 1, 1, "") // no annotation

	cl := fake.NewClientBuilder().WithScheme(newCNPGRestoreScheme(t)).
		WithObjects(bkp, rst, cluster).WithStatusSubresource(&cnpgv1.Cluster{}).Build()

	result, err := newCNPGRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending(), "expected Pending after initiating delete+recreate")

	// The new cluster should have the restore annotation
	var recreated cnpgv1.Cluster
	require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: "openfga-db", Namespace: cnpgClusterNS}, &recreated))
	assert.Equal(t, "bkp-openfga-db", recreated.Annotations["backup.platform-mesh.io/restored-from-cnpg-backup"])
}

// TestCNPGRestore_MultiCluster_AllReady verifies OK when all clusters are restored and ready.
func TestCNPGRestore_MultiCluster_AllReady(t *testing.T) {
	bkp := fakePlatformBackupWithCNPG("bkp", map[string]string{
		"openfga-db":  "bkp-openfga-db",
		"keycloak-db": "bkp-keycloak-db",
	})
	rst := fakePlatformRestoreCNPG("r", "bkp")
	c1 := fakeCNPGCluster("openfga-db", 1, 1, "bkp-openfga-db")
	c2 := fakeCNPGCluster("keycloak-db", 1, 1, "bkp-keycloak-db")

	cl := fake.NewClientBuilder().WithScheme(newCNPGRestoreScheme(t)).
		WithObjects(bkp, rst, c1, c2).WithStatusSubresource(&cnpgv1.Cluster{}).Build()

	result, err := newCNPGRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestCNPGRestore_ClusterNotFound verifies Pending is returned when the Cluster CR to restore is absent
// (transient — may occur after delete+create before the API server reflects the new object).
func TestCNPGRestore_ClusterNotFound(t *testing.T) {
	bkp := fakePlatformBackupWithCNPG("bkp", map[string]string{"openfga-db": "bkp-openfga-db"})
	rst := fakePlatformRestoreCNPG("r", "bkp")
	// no cluster CR created

	cl := fake.NewClientBuilder().WithScheme(newCNPGRestoreScheme(t)).WithObjects(bkp, rst).Build()

	result, err := newCNPGRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err, "transient NotFound must not surface as a permanent error")
	assert.True(t, result.IsPending(), "expected Pending while cluster is not yet visible")
}

// TestCNPGRestore_DeletionTimestamp_PendingWithFinalizers verifies Pending when cluster is terminating.
func TestCNPGRestore_DeletionTimestamp_PendingWithFinalizers(t *testing.T) {
	bkp := fakePlatformBackupWithCNPG("bkp", map[string]string{"openfga-db": "bkp-openfga-db"})
	rst := fakePlatformRestoreCNPG("r", "bkp")
	cluster := fakeCNPGCluster("openfga-db", 0, 1, "")
	now := metav1.Now()
	cluster.DeletionTimestamp = &now
	cluster.Finalizers = []string{"some-finalizer"}

	stripCalled := false
	cl := fake.NewClientBuilder().WithScheme(newCNPGRestoreScheme(t)).
		WithObjects(bkp, rst, cluster).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, patch ctrlruntimeclient.Patch, opts ...ctrlruntimeclient.PatchOption) error {
				if _, ok := obj.(*cnpgv1.Cluster); ok {
					stripCalled = true
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).Build()

	result, err := newCNPGRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending())
	assert.True(t, stripCalled, "expected finalizers to be stripped")
}

// TestCNPGRestore_GetName verifies the subroutine returns the expected condition name.
func TestCNPGRestore_GetName(t *testing.T) {
	assert.Equal(t, restore.ConditionCNPGRestored, newCNPGRestoreSub().GetName())
}

// TestCNPGRestore_GetBackupGenericError verifies that a non-NotFound error on Get for PlatformBackup
// is surfaced as an error containing "fetching source backup".
func TestCNPGRestore_GetBackupGenericError(t *testing.T) {
	rst := fakePlatformRestoreCNPG("r", "some-backup")

	cl := fake.NewClientBuilder().
		WithScheme(newCNPGRestoreScheme(t)).
		WithObjects(rst).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c ctrlruntimeclient.WithWatch, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
				if _, ok := obj.(*pmbackupv1alpha1.PlatformBackup); ok {
					return fmt.Errorf("internal server error: etcd timeout")
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	result, err := newCNPGRestoreSub().Process(ctxWithClient(cl), rst)
	assert.True(t, result.IsContinue())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching source backup")
}

// TestCNPGRestore_GetClusterGenericError verifies that a non-NotFound error on Get for cnpgv1.Cluster
// is surfaced as an error containing "fetching Cluster".
func TestCNPGRestore_GetClusterGenericError(t *testing.T) {
	bkp := fakePlatformBackupWithCNPG("bkp", map[string]string{"openfga-db": "bkp-openfga-db"})
	rst := fakePlatformRestoreCNPG("r", "bkp")

	cl := fake.NewClientBuilder().
		WithScheme(newCNPGRestoreScheme(t)).
		WithObjects(bkp, rst).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c ctrlruntimeclient.WithWatch, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
				if _, ok := obj.(*cnpgv1.Cluster); ok {
					return fmt.Errorf("connection refused: apiserver unavailable")
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	result, err := newCNPGRestoreSub().Process(ctxWithClient(cl), rst)
	assert.True(t, result.IsContinue())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching Cluster")
}

// TestCNPGRestore_CreateAlreadyExists_PatchAnnotation verifies that when Create returns AlreadyExists
// for a cluster that exists without a DeletionTimestamp, the operator patches the restore annotation
// onto the existing cluster and returns Pending.
func TestCNPGRestore_CreateAlreadyExists_PatchAnnotation(t *testing.T) {
	bkp := fakePlatformBackupWithCNPG("bkp", map[string]string{"openfga-db": "bkp-openfga-db"})
	rst := fakePlatformRestoreCNPG("r", "bkp")
	// Cluster exists without annotation and without DeletionTimestamp
	cluster := fakeCNPGCluster("openfga-db", 1, 1, "")

	patchCalled := false
	clusterGVR := schema.GroupResource{Group: "postgresql.cnpg.io", Resource: "clusters"}

	cl := fake.NewClientBuilder().
		WithScheme(newCNPGRestoreScheme(t)).
		WithObjects(bkp, rst, cluster).
		WithStatusSubresource(&cnpgv1.Cluster{}).
		WithInterceptorFuncs(interceptor.Funcs{
			// Make Delete a no-op so the cluster stays in the fake store for the subsequent
			// Get call in the AlreadyExists handler (simulating a race where the cluster
			// was recreated by another actor before our Delete could complete).
			Delete: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.DeleteOption) error {
				if _, ok := obj.(*cnpgv1.Cluster); ok {
					return nil // no-op: cluster persists
				}
				return c.Delete(ctx, obj, opts...)
			},
			Create: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
				if _, ok := obj.(*cnpgv1.Cluster); ok {
					return apierrors.NewAlreadyExists(clusterGVR, obj.GetName())
				}
				return c.Create(ctx, obj, opts...)
			},
			Patch: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, patch ctrlruntimeclient.Patch, opts ...ctrlruntimeclient.PatchOption) error {
				if cl, ok := obj.(*cnpgv1.Cluster); ok {
					if cl.Annotations != nil && cl.Annotations["backup.platform-mesh.io/restored-from-cnpg-backup"] == "bkp-openfga-db" {
						patchCalled = true
					}
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	result, err := newCNPGRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, patchCalled, "Patch must be called to apply restore annotation on the existing cluster")
	assert.True(t, result.IsPending(), "expected Pending after AlreadyExists — cluster readiness not yet confirmed")

	// Verify the annotation was actually persisted
	var updated cnpgv1.Cluster
	require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: "openfga-db", Namespace: cnpgClusterNS}, &updated))
	assert.Equal(t, "bkp-openfga-db", updated.Annotations["backup.platform-mesh.io/restored-from-cnpg-backup"])
}
