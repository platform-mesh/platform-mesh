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
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/internal/testfixtures"
	"go.platform-mesh.io/backup-operator/pkg/archive"
	"go.platform-mesh.io/backup-operator/pkg/restore"
	"go.platform-mesh.io/backup-operator/pkg/storage"
	subbackup "go.platform-mesh.io/backup-operator/pkg/subroutines/backup"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ns           = testfixtures.Namespace
	pmOperator   = "platform-mesh-operator"
	newerFull    = "/v2/Full-00000000-00000200-9999999999"
	newerDelta   = "/v2/Incr-00000201-00000250-9999999999"
	backupSnapID = "Full-00000000-00000100-1"
)

type env struct {
	t      *testing.T
	ctx    context.Context
	cl     ctrlruntimeclient.Client
	stores *storage.MemoryFactory
	x      *archive.Extracted
	// wait is called between the checks of a step, after kubelet and druid were played.
	wait func()
}

// newEnv takes a backup "daily" with snapshots at revision 100, then writes newer snapshots at
// revision 200 to the druid store, like druid does after the backup.
func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	extra := make([]ctrlruntimeclient.Object, 0, 1+len(testfixtures.Etcds))
	extra = append(extra, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: pmOperator, Namespace: ns},
		Spec:       appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)},
		Status:     appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1, UpdatedReplicas: 1},
	})
	for _, e := range testfixtures.Etcds {
		extra = append(extra, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
			Name: e + "-" + e + "-0", Namespace: ns,
			Labels: map[string]string{"app.kubernetes.io/part-of": e, "app.kubernetes.io/managed-by": "etcd-druid"},
		}})
	}
	cl := testfixtures.Cluster(t, extra...)
	stores := &storage.MemoryFactory{}

	b := &pmbackupv1alpha1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "daily", Namespace: ns},
		Spec:       pmbackupv1alpha1.BackupSpec{StorageRef: corev1.LocalObjectReference{Name: testfixtures.Storage}},
	}
	subs := subbackup.New(cl, stores, subbackup.FromBackupStorage(cl, stores))
	testfixtures.Run(ctx, t, subs, b)
	testfixtures.SimulateDruid(ctx, t, cl, stores, 100)
	testfixtures.Run(ctx, t, subs, b)
	require.Equal(t, pmbackupv1alpha1.PhaseCompleted, b.Status.Phase, b.Status.Message)

	data, err := storage.GetBytes(ctx, stores.Bucket(testfixtures.BackupBkt), b.Status.Archive)
	require.NoError(t, err)
	x, err := archive.Extract(bytes.NewReader(data), testfixtures.Key, t.TempDir())
	require.NoError(t, err)

	for _, e := range testfixtures.Etcds {
		require.NoError(t, storage.PutBytes(ctx, stores.Bucket(testfixtures.EtcdBucket), testfixtures.StorePrefix(e)+newerFull, []byte("newer")))
		require.NoError(t, storage.PutBytes(ctx, stores.Bucket(testfixtures.EtcdBucket), testfixtures.StorePrefix(e)+newerDelta, []byte("delta")))
	}
	return &env{t: t, ctx: ctx, cl: cl, stores: stores, x: x}
}

func (e *env) run(parts ...restore.Part) error {
	return restore.New(e.cl, e.stores, e.x, restore.Options{
		Namespace:            ns,
		KcpOperator:          testfixtures.KcpOperator,
		PlatformMeshOperator: pmOperator,
		Parts:                parts,
		SettleTime:           time.Millisecond,
		Timeout:              10 * time.Second,
		Wait: func(context.Context) error {
			e.simulateKubelet()
			if e.wait != nil {
				e.wait()
			}
			return nil
		},
		Log: e.t.Logf,
	}).Run(e.ctx)
}

// simulateKubelet sets the deployment status to the spec, and creates ready pods for started etcds.
func (e *env) simulateKubelet() {
	deps := &appsv1.DeploymentList{}
	require.NoError(e.t, e.cl.List(e.ctx, deps))
	for i := range deps.Items {
		d := &deps.Items[i]
		n := ptr.Deref(d.Spec.Replicas, 1)
		d.Status = appsv1.DeploymentStatus{Replicas: n, ReadyReplicas: n, UpdatedReplicas: n}
		require.NoError(e.t, e.cl.Status().Update(e.ctx, d))
	}
	for _, name := range testfixtures.Etcds {
		etcd := e.etcd(name)
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name + "-0", Namespace: ns,
			Labels: map[string]string{"app.kubernetes.io/part-of": name, "app.kubernetes.io/managed-by": "etcd-druid"}}}
		if etcd.Spec.Replicas == 0 {
			require.NoError(e.t, ctrlruntimeclient.IgnoreNotFound(e.cl.Delete(e.ctx, pod)))
			continue
		}
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		if err := e.cl.Create(e.ctx, pod); err != nil {
			require.True(e.t, apierrors.IsAlreadyExists(err))
		}
		etcd.Status.Ready = ptr.To(true)
		etcd.Status.ReadyReplicas = etcd.Spec.Replicas
		require.NoError(e.t, e.cl.Status().Update(e.ctx, etcd))
	}
}

func (e *env) etcd(name string) *druidv1alpha1.Etcd {
	etcd := &druidv1alpha1.Etcd{}
	require.NoError(e.t, e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: name}, etcd))
	return etcd
}

func (e *env) deployment(name string) *appsv1.Deployment {
	d := &appsv1.Deployment{}
	require.NoError(e.t, e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: name}, d))
	return d
}

func (e *env) replicas(name string) int32 {
	return ptr.Deref(e.deployment(name).Spec.Replicas, 1)
}

func (e *env) secretData(name string) string {
	s := &corev1.Secret{}
	require.NoError(e.t, e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: name}, s))
	return string(s.Data["tls.crt"])
}

func (e *env) setSecret(name, crt string) {
	s := &corev1.Secret{}
	require.NoError(e.t, e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: name}, s))
	s.Data["tls.crt"] = []byte(crt)
	require.NoError(e.t, e.cl.Update(e.ctx, s))
}

func (e *env) kubeconfig() *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "operator.kcp.io", Version: "v1alpha1", Kind: "Kubeconfig"})
	u.SetNamespace(ns)
	u.SetName(testfixtures.Kubeconfig)
	return u
}

func (e *env) noAnnotations() {
	e.t.Helper()
	deps := &appsv1.DeploymentList{}
	require.NoError(e.t, e.cl.List(e.ctx, deps))
	for _, d := range deps.Items {
		assert.NotContains(e.t, d.Annotations, restore.ReplicasAnnotation, d.Name)
	}
	etcds := &druidv1alpha1.EtcdList{}
	require.NoError(e.t, e.cl.List(e.ctx, etcds))
	for _, etcd := range etcds.Items {
		assert.NotContains(e.t, etcd.Annotations, restore.ReplicasAnnotation, etcd.Name)
	}
}

func TestRestoreAll(t *testing.T) {
	e := newEnv(t)
	e.setSecret("root-ca", "new-ca")
	require.NoError(t, e.cl.Delete(e.ctx, &certmanagerv1.Certificate{ObjectMeta: metav1.ObjectMeta{Name: "root-server", Namespace: ns}}))
	require.NoError(t, e.cl.Delete(e.ctx, e.kubeconfig()))

	// kcp starts before its clients and platform-mesh-operator
	kcpFirst := false
	e.wait = func() {
		if e.replicas("root-kcp") == 1 && e.replicas(testfixtures.KcpOperator) == 1 &&
			e.replicas(testfixtures.KcpClient) == 0 && e.replicas(pmOperator) == 0 {
			kcpFirst = true
		}
	}
	require.NoError(t, e.run())
	assert.True(t, kcpFirst)

	assert.Equal(t, "crt-root-ca", e.secretData("root-ca"))
	require.NoError(t, e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: "root-server"}, &certmanagerv1.Certificate{}))
	kc := e.kubeconfig()
	require.NoError(t, e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: testfixtures.Kubeconfig}, kc))
	assert.NotContains(t, kc.Object, "status")
	name, _, _ := unstructured.NestedString(kc.Object, "spec", "secretRef", "name")
	assert.Equal(t, "kubeconfig-cert-"+testfixtures.Kubeconfig, name)

	etcdBucket := e.stores.Bucket(testfixtures.EtcdBucket)
	for _, name := range testfixtures.Etcds {
		prefix := testfixtures.StorePrefix(name)
		assert.Equal(t, []string{prefix + "/v2/" + backupSnapID}, testfixtures.KeysWithPrefix(etcdBucket, prefix+"/"),
			"only the backup snapshot is in the druid store of %s", name)
		var before []string
		for _, k := range testfixtures.KeysWithPrefix(etcdBucket, "before-restore-") {
			if strings.Contains(k, "/"+prefix+"/") {
				before = append(before, k)
			}
		}
		assert.Len(t, before, 3, "the old snapshots of %s are kept", name)
		assert.Equal(t, int32(1), e.etcd(name).Spec.Replicas)
	}

	for name, want := range map[string]int32{
		pmOperator: 1, testfixtures.KcpOperator: 1, "root-kcp": 1, "triton-shard-kcp": 1,
		"frontproxy-front-proxy": 2, testfixtures.KcpClient: 1, "openfga": 1,
	} {
		assert.Equal(t, want, e.replicas(name), name)
	}
	e.noAnnotations()
}

func TestRestoreOnlySecrets(t *testing.T) {
	e := newEnv(t)
	e.setSecret("root-server", "new-server")
	etcdStopped := false
	e.wait = func() {
		if e.etcd("etcd-kcp").Spec.Replicas == 0 {
			etcdStopped = true
		}
	}
	require.NoError(t, e.run(restore.PartSecrets))

	assert.Equal(t, "crt-root-server", e.secretData("root-server"))
	assert.False(t, etcdStopped)
	keys := testfixtures.KeysWithPrefix(e.stores.Bucket(testfixtures.EtcdBucket), testfixtures.StorePrefix("etcd-kcp")+"/")
	assert.Contains(t, keys, testfixtures.StorePrefix("etcd-kcp")+newerFull, "the druid store is not changed")
	assert.Equal(t, int32(2), e.replicas("frontproxy-front-proxy"))
	e.noAnnotations()
}

// TestRestoreRunsAgain continues a restore that stopped after platform mesh was scaled down.
func TestRestoreRunsAgain(t *testing.T) {
	e := newEnv(t)
	d := e.deployment("frontproxy-front-proxy")
	d.Annotations = map[string]string{restore.ReplicasAnnotation: "2"}
	d.Spec.Replicas = ptr.To[int32](0)
	require.NoError(t, e.cl.Update(e.ctx, d))

	require.NoError(t, e.run(restore.PartCertificates))
	assert.Equal(t, int32(2), e.replicas("frontproxy-front-proxy"))
	e.noAnnotations()
}

func TestRestorePreflightFails(t *testing.T) {
	tests := map[string]struct {
		change func(e *env)
		msg    string
	}{
		"shard is missing": {
			change: func(e *env) {
				u := &unstructured.Unstructured{}
				u.SetAPIVersion("operator.kcp.io/v1alpha1")
				u.SetKind("Shard")
				u.SetNamespace(ns)
				u.SetName("triton")
				require.NoError(e.t, e.cl.Delete(e.ctx, u))
			},
			msg: "kcp topology is not the same as in the backup: Shard: backup has [triton], cluster has []",
		},
		"store prefix changed": {
			change: func(e *env) {
				etcd := e.etcd("etcd-kcp")
				etcd.Spec.Backup.Store.Prefix = "other"
				require.NoError(e.t, e.cl.Update(e.ctx, etcd))
			},
			msg: "etcd etcd-kcp: store prefix is other, the backup has etcd-kcp-store",
		},
		"etcd is missing": {
			change: func(e *env) {
				require.NoError(e.t, e.cl.Delete(e.ctx, e.etcd("etcd-cache")))
			},
			msg: "etcd etcd-cache from the backup does not exist",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			tc.change(e)
			err := e.run()
			require.Error(t, err)
			assert.Equal(t, "preflight: "+tc.msg, err.Error())
			assert.Equal(t, int32(1), e.replicas(testfixtures.KcpOperator), "nothing is stopped")
			e.noAnnotations()
		})
	}
}

func TestParseParts(t *testing.T) {
	parts, err := restore.ParseParts(nil)
	require.NoError(t, err)
	assert.Equal(t, restore.AllParts, parts)

	parts, err = restore.ParseParts([]string{"etcd", "secrets"})
	require.NoError(t, err)
	assert.Equal(t, []restore.Part{restore.PartSecrets, restore.PartEtcd}, parts, "restore order")

	_, err = restore.ParseParts([]string{"etcd", "pods"})
	assert.True(t, err != nil && strings.Contains(err.Error(), `unknown part "pods"`))
}
