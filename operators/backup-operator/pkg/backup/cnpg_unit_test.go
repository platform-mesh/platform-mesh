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

package backup_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const cnpgClusterNamespace = "cnpg-system"

func newCNPGScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, pmbackupv1alpha1.AddToScheme(s))
	require.NoError(t, cnpgv1.AddToScheme(s))
	return s
}

func cnpgCtxWithClient(cl ctrlruntimeclient.Client) context.Context {
	ctx := subroutines.WithClient(context.Background(), cl)
	return logger.SetLoggerInContext(ctx, testlogger.New().Logger)
}

func fakeCNPGBackup(backupName, clusterName string, phase cnpgv1.BackupPhase) *cnpgv1.Backup {
	b := &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backupName + "-" + clusterName,
			Namespace: cnpgClusterNamespace,
		},
		Spec: cnpgv1.BackupSpec{
			Cluster: cnpgv1.LocalObjectReference{Name: clusterName},
			Method:  cnpgv1.BackupMethodBarmanObjectStore,
		},
	}
	b.Status.Phase = phase
	return b
}

func fakePlatformBackupCNPG(name string, cnpgEnabled bool) *pmbackupv1alpha1.PlatformBackup {
	return &pmbackupv1alpha1.PlatformBackup{
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
				Etcd: pmbackupv1alpha1.EtcdSpec{Enabled: false},
				CNPG: pmbackupv1alpha1.CNPGSpec{Enabled: cnpgEnabled},
			},
		},
	}
}

func newCNPGSub(clusters ...string) *backup.CNPGCaptureSubroutine {
	if len(clusters) == 0 {
		clusters = []string{"openfga-db", "keycloak-db"}
	}
	return backup.NewCNPGCaptureSubroutine(cnpgClusterNamespace, clusters).
		WithPollIntervals(1*time.Millisecond, 5*time.Second)
}

// TestCNPGCapture_Disabled verifies Process is a no-op when CNPG is disabled.
func TestCNPGCapture_Disabled(t *testing.T) {
	bkp := fakePlatformBackupCNPG("bkp", false)
	cl := fake.NewClientBuilder().WithScheme(newCNPGScheme(t)).WithObjects(bkp).Build()

	result, err := newCNPGSub().Process(cnpgCtxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
	assert.Nil(t, bkp.Status.Artefacts.CNPG)
}

// TestCNPGCapture_Idempotent verifies a second call is a no-op when artefacts are already recorded.
func TestCNPGCapture_Idempotent(t *testing.T) {
	bkp := fakePlatformBackupCNPG("bkp", true)
	bkp.Status.Artefacts.CNPG = &pmbackupv1alpha1.CNPGArtefact{
		Backups: map[string]string{"openfga-db": "bkp-openfga-db"},
	}
	cl := fake.NewClientBuilder().WithScheme(newCNPGScheme(t)).WithObjects(bkp).Build()

	result, err := newCNPGSub("openfga-db").Process(cnpgCtxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
	assert.Equal(t, "bkp-openfga-db", bkp.Status.Artefacts.CNPG.Backups["openfga-db"])
}

// TestCNPGCapture_NoClusters verifies StopWithRequeue is returned when dynamic discovery finds no clusters.
func TestCNPGCapture_NoClusters(t *testing.T) {
	bkp := fakePlatformBackupCNPG("bkp", true)
	cl := fake.NewClientBuilder().WithScheme(newCNPGScheme(t)).WithObjects(bkp).Build()

	sub := backup.NewCNPGCaptureSubroutine(cnpgClusterNamespace, []string{}).
		WithPollIntervals(1*time.Millisecond, 5*time.Second)
	result, err := sub.Process(cnpgCtxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue())
}

// TestCNPGCapture_WrongObjectType verifies an error is returned for non-PlatformBackup objects.
func TestCNPGCapture_WrongObjectType(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newCNPGScheme(t)).Build()
	restore := &pmbackupv1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{Name: "r"}}
	_, err := newCNPGSub().Process(cnpgCtxWithClient(cl), restore)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected object type")
}

// TestCNPGCapture_SingleCluster_Success verifies a completed Backup CR is recorded in artefacts.
func TestCNPGCapture_SingleCluster_Success(t *testing.T) {
	bkp := fakePlatformBackupCNPG("bkp", true)
	existingBackup := fakeCNPGBackup("bkp", "openfga-db", cnpgv1.BackupPhaseCompleted)

	cl := fake.NewClientBuilder().
		WithScheme(newCNPGScheme(t)).
		WithObjects(bkp, existingBackup).
		WithStatusSubresource(&cnpgv1.Backup{}).
		Build()

	result, err := newCNPGSub("openfga-db").Process(cnpgCtxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
	require.NotNil(t, bkp.Status.Artefacts.CNPG)
	assert.Equal(t, "bkp-openfga-db", bkp.Status.Artefacts.CNPG.Backups["openfga-db"])
}

// TestCNPGCapture_MultiCluster_Success verifies all clusters are captured and recorded.
func TestCNPGCapture_MultiCluster_Success(t *testing.T) {
	bkp := fakePlatformBackupCNPG("bkp", true)
	b1 := fakeCNPGBackup("bkp", "openfga-db", cnpgv1.BackupPhaseCompleted)
	b2 := fakeCNPGBackup("bkp", "keycloak-db", cnpgv1.BackupPhaseCompleted)

	cl := fake.NewClientBuilder().
		WithScheme(newCNPGScheme(t)).
		WithObjects(bkp, b1, b2).
		WithStatusSubresource(&cnpgv1.Backup{}).
		Build()

	result, err := newCNPGSub("openfga-db", "keycloak-db").Process(cnpgCtxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
	require.NotNil(t, bkp.Status.Artefacts.CNPG)
	assert.Equal(t, "bkp-openfga-db", bkp.Status.Artefacts.CNPG.Backups["openfga-db"])
	assert.Equal(t, "bkp-keycloak-db", bkp.Status.Artefacts.CNPG.Backups["keycloak-db"])
}

// TestCNPGCapture_TaskFailed_ReturnsError verifies a failed Backup CR surfaces as an error.
func TestCNPGCapture_TaskFailed_ReturnsError(t *testing.T) {
	bkp := fakePlatformBackupCNPG("bkp", true)
	b := fakeCNPGBackup("bkp", "openfga-db", cnpgv1.BackupPhaseFailed)
	b.Status.Error = "disk full"

	cl := fake.NewClientBuilder().
		WithScheme(newCNPGScheme(t)).
		WithObjects(bkp, b).
		WithStatusSubresource(&cnpgv1.Backup{}).
		Build()

	_, err := newCNPGSub("openfga-db").Process(cnpgCtxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

// TestCNPGCapture_StaysPendingWhileBackupInProgress verifies Pending is returned when the Backup CR is not yet complete.
func TestCNPGCapture_StaysPendingWhileBackupInProgress(t *testing.T) {
	bkp := fakePlatformBackupCNPG("bkp", true)
	b := fakeCNPGBackup("bkp", "openfga-db", cnpgv1.BackupPhasePending)

	cl := fake.NewClientBuilder().
		WithScheme(newCNPGScheme(t)).
		WithObjects(bkp, b).
		WithStatusSubresource(&cnpgv1.Backup{}).
		Build()

	sub := backup.NewCNPGCaptureSubroutine(cnpgClusterNamespace, []string{"openfga-db"}).
		WithPollIntervals(100*time.Millisecond, 5*time.Second)

	result, err := sub.Process(cnpgCtxWithClient(cl), bkp)
	require.NoError(t, err, "non-blocking: pending backup must not return an error")
	assert.True(t, result.IsPending(), "expected Pending result while backup is in progress")
}

// TestCNPGCapture_BackupCRCreatedWhenAbsent verifies a Backup CR is created if it does not exist yet.
func TestCNPGCapture_BackupCRCreatedWhenAbsent(t *testing.T) {
	bkp := fakePlatformBackupCNPG("bkp", true)

	created := false
	cl := fake.NewClientBuilder().
		WithScheme(newCNPGScheme(t)).
		WithObjects(bkp).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
				if _, ok := obj.(*cnpgv1.Backup); ok {
					created = true
					// inject completed status so poll returns immediately
					b := obj.(*cnpgv1.Backup).DeepCopy()
					b.Status.Phase = cnpgv1.BackupPhaseCompleted
					if err := c.Create(ctx, b, opts...); err != nil {
						return err
					}
					return nil
				}
				return c.Create(ctx, obj, opts...)
			},
		}).
		Build()

	sub := backup.NewCNPGCaptureSubroutine(cnpgClusterNamespace, []string{"openfga-db"}).
		WithPollIntervals(1*time.Millisecond, 5*time.Second)

	_, _ = sub.Process(cnpgCtxWithClient(cl), bkp)
	assert.True(t, created, "expected Backup CR to be created")

	var b cnpgv1.Backup
	err := cl.Get(t.Context(), types.NamespacedName{Name: "bkp-openfga-db", Namespace: cnpgClusterNamespace}, &b)
	assert.NoError(t, err)
}

// TestCNPGCapture_GetName verifies the subroutine returns the expected condition name.
func TestCNPGCapture_GetName(t *testing.T) {
	assert.Equal(t, backup.ConditionCNPGSnapshotted, newCNPGSub().GetName())
}

// TestCNPGCapture_ListClustersError verifies that when dynamic discovery is used (clusters=nil)
// and the List call returns an error, Process returns the error containing "listing CNPG clusters".
func TestCNPGCapture_ListClustersError(t *testing.T) {
	bkp := fakePlatformBackupCNPG("bkp", true)

	cl := fake.NewClientBuilder().
		WithScheme(newCNPGScheme(t)).
		WithObjects(bkp).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, c ctrlruntimeclient.WithWatch, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
				if _, ok := list.(*cnpgv1.ClusterList); ok {
					return fmt.Errorf("internal server error")
				}
				return c.List(ctx, list, opts...)
			},
		}).
		Build()

	// Use dynamic-discovery mode: pass nil/empty clusters list so it calls List.
	sub := backup.NewCNPGCaptureSubroutine(cnpgClusterNamespace, nil).
		WithPollIntervals(1*time.Millisecond, 5*time.Second)

	_, err := sub.Process(cnpgCtxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing CNPG clusters")
}

// TestCNPGCapture_CreateBackupCRFails verifies that when the Create call for a cnpgv1.Backup
// returns an error, Process returns that error.
func TestCNPGCapture_CreateBackupCRFails(t *testing.T) {
	bkp := fakePlatformBackupCNPG("bkp", true)

	cl := fake.NewClientBuilder().
		WithScheme(newCNPGScheme(t)).
		WithObjects(bkp).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
				if _, ok := obj.(*cnpgv1.Backup); ok {
					return fmt.Errorf("quota exceeded")
				}
				return c.Create(ctx, obj, opts...)
			},
		}).
		Build()

	_, err := newCNPGSub("openfga-db").Process(cnpgCtxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota exceeded")
}

// TestCNPGCapture_MultiCluster_PartialFailure verifies that when one cluster's Backup CR has
// phase Failed and another has phase Completed, Process returns an error containing the failing
// cluster name and does not write partial artefacts.
func TestCNPGCapture_MultiCluster_PartialFailure(t *testing.T) {
	bkp := fakePlatformBackupCNPG("bkp", true)
	// openfga-db backup has Failed — error message includes "disk full"
	failedBackup := fakeCNPGBackup("bkp", "openfga-db", cnpgv1.BackupPhaseFailed)
	failedBackup.Status.Error = "disk full"
	// keycloak-db backup has Completed
	completedBackup := fakeCNPGBackup("bkp", "keycloak-db", cnpgv1.BackupPhaseCompleted)

	cl := fake.NewClientBuilder().
		WithScheme(newCNPGScheme(t)).
		WithObjects(bkp, failedBackup, completedBackup).
		WithStatusSubresource(&cnpgv1.Backup{}).
		Build()

	_, err := newCNPGSub("openfga-db", "keycloak-db").Process(cnpgCtxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openfga-db")
	// Artefacts must not be partially written when any cluster failed.
	assert.Nil(t, bkp.Status.Artefacts.CNPG)
}
