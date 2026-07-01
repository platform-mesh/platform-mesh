//go:build integration

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
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/restore"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// runCNPGClusterReadySimulator watches for Cluster CRs and sets them ready.
func runCNPGClusterReadySimulator(ctx context.Context, cl client.Client) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
			var list cnpgv1.ClusterList
			if err := cl.List(ctx, &list, client.InNamespace(testNamespace)); err != nil {
				continue
			}
			for i := range list.Items {
				c := &list.Items[i]
				if c.Status.ReadyInstances == c.Spec.Instances && c.Spec.Instances > 0 {
					continue
				}
				patch := client.MergeFrom(c.DeepCopy())
				c.Status.ReadyInstances = c.Spec.Instances
				_ = cl.Status().Patch(ctx, c, patch)
			}
		}
	}()
}

func makeBackupWithCNPG(t *testing.T, cl client.Client, name string, cnpgBackups map[string]string) *backupv1alpha1.PlatformBackup {
	t.Helper()
	bkp := &backupv1alpha1.PlatformBackup{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: backupv1alpha1.PlatformBackupSpec{
			Storage: backupv1alpha1.StorageSpec{
				S3: backupv1alpha1.S3StorageSpec{
					Endpoint:       "http://minio:9000",
					Bucket:         "backups",
					CredentialsRef: corev1.LocalObjectReference{Name: "s3-credentials"},
				},
			},
			Components: backupv1alpha1.ComponentsSpec{
				CNPG: backupv1alpha1.CNPGSpec{Enabled: true},
			},
		},
	}
	require.NoError(t, cl.Create(t.Context(), bkp))
	bkp.Status.Artefacts.CNPG = &backupv1alpha1.CNPGArtefact{Backups: cnpgBackups}
	require.NoError(t, cl.Status().Update(t.Context(), bkp))
	return bkp
}

func makeCNPGClusterForRestore(t *testing.T, cl client.Client, name string) *cnpgv1.Cluster {
	t.Helper()
	c := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec:       cnpgv1.ClusterSpec{Instances: 1},
	}
	require.NoError(t, cl.Create(t.Context(), c))
	// Set ready.
	patch := client.MergeFrom(c.DeepCopy())
	c.Status.ReadyInstances = 1
	_ = cl.Status().Patch(t.Context(), c, patch)
	return c
}

// TestCNPGRestore_SingleCluster_Integration verifies a Cluster is deleted, recreated, and annotated.
func TestCNPGRestore_SingleCluster_Integration(t *testing.T) {
	cl := newCNPGTestClient(t)
	stop := func() {}
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	runFinalizerCleaner(ctx, cl)
	runCNPGClusterReadySimulator(ctx, cl)

	makeCNPGClusterForRestore(t, cl, "openfga-db")
	bkp := makeBackupWithCNPG(t, cl, "backup-cnpg-rst", map[string]string{
		"openfga-db": "backup-cnpg-rst-openfga-db",
	})
	rst := makePlatformRestore(t, cl, "restore-cnpg", bkp.Name)

	sub := restore.NewCNPGRestoreSubroutine(testNamespace, testNamespace)

	require.Eventually(t, func() bool {
		result, err := sub.Process(injectClient(ctx, cl), rst)
		require.NoError(t, err)
		return result.IsContinue()
	}, 20*time.Second, 100*time.Millisecond)

	var recreated cnpgv1.Cluster
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: "openfga-db", Namespace: testNamespace}, &recreated))
	assert.Equal(t, "backup-cnpg-rst-openfga-db",
		recreated.Annotations["backup.platform-mesh.io/restored-from-cnpg-backup"])
	require.NotNil(t, recreated.Spec.Bootstrap)
	require.NotNil(t, recreated.Spec.Bootstrap.Recovery)
	require.NotNil(t, recreated.Spec.Bootstrap.Recovery.Backup)
	assert.Equal(t, "backup-cnpg-rst-openfga-db", recreated.Spec.Bootstrap.Recovery.Backup.Name)
}

// TestCNPGRestore_MultiCluster_Integration verifies all clusters are restored.
func TestCNPGRestore_MultiCluster_Integration(t *testing.T) {
	cl := newCNPGTestClient(t)
	stop := func() {}
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	runFinalizerCleaner(ctx, cl)
	runCNPGClusterReadySimulator(ctx, cl)

	makeCNPGClusterForRestore(t, cl, "openfga-db")
	makeCNPGClusterForRestore(t, cl, "keycloak-db")
	bkp := makeBackupWithCNPG(t, cl, "backup-cnpg-multi-rst", map[string]string{
		"openfga-db":  "backup-cnpg-multi-rst-openfga-db",
		"keycloak-db": "backup-cnpg-multi-rst-keycloak-db",
	})
	rst := makePlatformRestore(t, cl, "restore-cnpg-multi", bkp.Name)

	sub := restore.NewCNPGRestoreSubroutine(testNamespace, testNamespace)

	require.Eventually(t, func() bool {
		result, err := sub.Process(injectClient(ctx, cl), rst)
		require.NoError(t, err)
		return result.IsContinue()
	}, 30*time.Second, 100*time.Millisecond)

	for _, name := range []string{"openfga-db", "keycloak-db"} {
		var c cnpgv1.Cluster
		require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, &c))
		assert.NotEmpty(t, c.Annotations["backup.platform-mesh.io/restored-from-cnpg-backup"])
	}
}

// TestCNPGRestore_MissingBackup_StopsWithRequeue verifies StopWithRequeue for missing backup.
func TestCNPGRestore_MissingBackup_StopsWithRequeue(t *testing.T) {
	cl := newCNPGTestClient(t)
	stop := func() {}
	defer stop()

	rst := makePlatformRestore(t, cl, "restore-cnpg-missing", "nonexistent")
	sub := restore.NewCNPGRestoreSubroutine(testNamespace, testNamespace)
	result, err := sub.Process(injectClient(t.Context(), cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue())
}

// TestCNPGRestore_NoCNPGArtefacts_Skips verifies OK when backup has no CNPG artefacts.
func TestCNPGRestore_NoCNPGArtefacts_Skips(t *testing.T) {
	cl := newCNPGTestClient(t)
	stop := func() {}
	defer stop()

	bkp := &backupv1alpha1.PlatformBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "bkp-no-cnpg"},
		Spec: backupv1alpha1.PlatformBackupSpec{
			Storage: backupv1alpha1.StorageSpec{S3: backupv1alpha1.S3StorageSpec{
				Endpoint: "http://minio:9000", Bucket: "b",
				CredentialsRef: corev1.LocalObjectReference{Name: "s"},
			}},
		},
	}
	require.NoError(t, cl.Create(t.Context(), bkp))
	rst := makePlatformRestore(t, cl, "restore-cnpg-skip", bkp.Name)
	sub := restore.NewCNPGRestoreSubroutine(testNamespace, testNamespace)
	result, err := sub.Process(injectClient(t.Context(), cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestCNPGRestore_WaitForReady_Integration verifies Pending while cluster instances not ready.
func TestCNPGRestore_WaitForReady_Integration(t *testing.T) {
	cl := newCNPGTestClient(t)
	stop := func() {}
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	runFinalizerCleaner(ctx, cl)

	// Create original cluster
	orig := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "openfga-db", Namespace: testNamespace},
		Spec:       cnpgv1.ClusterSpec{Instances: 1},
	}
	require.NoError(t, cl.Create(ctx, orig))
	// Pre-annotate so we skip delete+recreate and go straight to "annotation set, not ready"
	patch := client.MergeFrom(orig.DeepCopy())
	if orig.Annotations == nil {
		orig.Annotations = map[string]string{}
	}
	orig.Annotations["backup.platform-mesh.io/restored-from-cnpg-backup"] = "bkp-openfga-db"
	require.NoError(t, cl.Patch(ctx, orig, patch))
	// Status: 0/1 ready
	statusPatch := client.MergeFrom(orig.DeepCopy())
	orig.Status.ReadyInstances = 0
	_ = cl.Status().Patch(ctx, orig, statusPatch)

	bkp := makeBackupWithCNPG(t, cl, "bkp", map[string]string{"openfga-db": "bkp-openfga-db"})
	rst := makePlatformRestore(t, cl, "restore-cnpg-wait", bkp.Name)

	sub := restore.NewCNPGRestoreSubroutine(testNamespace, testNamespace)
	result, err := sub.Process(injectClient(ctx, cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending())

	// Now set ready — should proceed to OK
	var c cnpgv1.Cluster
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: "openfga-db", Namespace: testNamespace}, &c))
	readyPatch := client.MergeFrom(c.DeepCopy())
	c.Status.ReadyInstances = 1
	_ = cl.Status().Patch(ctx, &c, readyPatch)

	require.Eventually(t, func() bool {
		result, err := sub.Process(injectClient(ctx, cl), rst)
		require.NoError(t, err)
		return result.IsContinue()
	}, 10*time.Second, 100*time.Millisecond)
}

func newCNPGTestClient(t *testing.T) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("adding clientgo scheme: %v", err)
	}
	if err := backupv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("adding backup scheme: %v", err)
	}
	if err := cnpgv1.AddToScheme(s); err != nil {
		t.Fatalf("adding cnpg scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&cnpgv1.Cluster{}, &backupv1alpha1.PlatformBackup{}).Build()
}
