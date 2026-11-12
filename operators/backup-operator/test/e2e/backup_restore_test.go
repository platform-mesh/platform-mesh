//go:build e2e

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

package e2e

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/druid"
	"go.platform-mesh.io/backup-operator/pkg/pki"
	"go.platform-mesh.io/backup-operator/pkg/restore"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	dataWorkspaces = []string{"bk-root", "bk-triton"}
	dataAccounts   = []string{"bk-org"}
)

// TestBackupRestore takes a backup of a populated Platform Mesh with a Backup object and with the
// backup command. Then it loses and restores etcd, secrets, certificates and everything, one after
// the other, with the restore command. After each restore Platform Mesh must work like before.
func TestBackupRestore(t *testing.T) {
	if os.Getenv("E2E_FRESH") != "" {
		t.Skip("E2E_FRESH is set, only the fresh cluster test runs")
	}
	e := newEnv(t)
	e.waitHealthy(20 * time.Minute)
	store := e.prepareStorage(t.TempDir())

	want := e.populate(store.Prefix)
	e.logf("state before backup: %d objects, fingerprints of %d secrets", len(want.Objects), len(want.Fingerprints))

	backup := e.takeBackup("e2e")
	e.logf("backup: %s, %d etcds, %d secrets, %d certificates", backup.Status.Archive, len(backup.Status.Etcds), backup.Status.Secrets, backup.Status.Certificates)
	e.cliBackup(store, "e2e-cli")
	keys := e.aws("s3", "ls", "--recursive", fmt.Sprintf("s3://%s/%s/", backupBucket, store.Prefix))
	assert.Contains(t, keys, store.Prefix+"/"+ns+"/e2e.tar")
	assert.Contains(t, keys, store.Prefix+"/"+ns+"/e2e-cli.tar")
	assert.Equal(t, 2, strings.Count(keys, "\n"), "one archive for each backup: %s", keys)

	t.Run("only etcd lost", func(t *testing.T) {
		e.createWorkspace("root", "bk-after-backup", "root")
		e.loseEtcd()
		require.NoError(t, e.cliRestore(store, "e2e", "etcd"))
		e.verify(t, want, "etcd")

		_, err := e.kcp("root").Resource(workspaceGVR).Get(e.ctx, "bk-after-backup", metav1.GetOptions{})
		assert.True(t, apierrors.IsNotFound(err), "a workspace made after the backup is gone: %v", err)
	})

	t.Run("only secrets lost", func(t *testing.T) {
		e.loseSecrets()
		require.NoError(t, e.cliRestore(store, "e2e", "secrets"))
		e.verify(t, want, "secrets")
	})

	t.Run("only certificates lost", func(t *testing.T) {
		e.scaleDeployment("kcp-operator", 0)
		names := e.loseCertificates()
		require.NoError(t, e.cliRestore(store, "e2e", "certificates"))
		for _, name := range names {
			require.NoError(t, e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: name}, &certmanagerv1.Certificate{}),
				"certificate %s is restored by the restore, kcp-operator is still stopped", name)
		}
		// the restore brings kcp-operator back to the replicas it had, 0 here
		e.scaleDeployment("kcp-operator", 1)
		e.verify(t, want, "certificates")
	})

	t.Run("everything lost, archive of the backup command", func(t *testing.T) {
		e.loseEtcd()
		e.loseSecrets()
		e.loseCertificates()
		require.NoError(t, e.cliRestore(store, "e2e-cli"))
		e.verify(t, want, "all")
	})

	t.Run("backup fails when an etcd is not ready", func(t *testing.T) {
		etcd := &druidv1alpha1.Etcd{}
		require.NoError(t, e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: "etcd-cache"}, etcd))
		require.NoError(t, druid.Scale(e.ctx, e.cl, etcd, 0))
		e.eventually(5*time.Minute, "etcd-cache not ready", func() (string, error) {
			if err := e.cl.Get(e.ctx, ctrlruntimeclient.ObjectKeyFromObject(etcd), etcd); err != nil {
				return "", err
			}
			if druid.IsReady(etcd) && etcd.Status.ReadyReplicas > 0 {
				return "still ready", nil
			}
			return "", nil
		})

		b := &pmbackupv1alpha1.Backup{
			ObjectMeta: metav1.ObjectMeta{Name: "e2e-not-ready", Namespace: ns},
			Spec:       pmbackupv1alpha1.BackupSpec{StorageRef: corev1.LocalObjectReference{Name: backupStore}},
		}
		require.NoError(t, e.cl.Create(e.ctx, b))
		e.waitBackup(b)
		assert.Equal(t, pmbackupv1alpha1.PhaseFailed, b.Status.Phase)
		assert.Equal(t, "etcd etcd-cache is not ready", b.Status.Message)

		out, err := e.cli(store, "backup", "--name", "e2e-cli-not-ready")
		require.Error(t, err)
		assert.Contains(t, out, "etcd etcd-cache is not ready")

		require.NoError(t, e.cl.Get(e.ctx, ctrlruntimeclient.ObjectKeyFromObject(etcd), etcd))
		require.NoError(t, druid.Scale(e.ctx, e.cl, etcd, 1))
		e.waitHealthy(10 * time.Minute)
	})

	t.Run("restore with a wrong key fails and changes nothing", func(t *testing.T) {
		wrong := store
		wrong.KeyFile = filepath.Join(t.TempDir(), "wrong.key")
		require.NoError(t, os.WriteFile(wrong.KeyFile, []byte(strings.Repeat("x", 32)), 0o600))

		out, err := e.cli(wrong, "restore", "--name", "e2e")
		require.Error(t, err)
		assert.Contains(t, out, "wrong key or broken data")
		e.notStopped(t)
		assert.Empty(t, e.diff(want, dataWorkspaces, dataAccounts))
	})

	t.Run("restore of a missing backup fails and changes nothing", func(t *testing.T) {
		out, err := e.cli(store, "restore", "--name", "does-not-exist")
		require.Error(t, err)
		assert.Contains(t, out, "object not found")
		e.notStopped(t)
	})
}

// notStopped checks that a restore did not stop anything.
func (e *env) notStopped(t *testing.T) {
	t.Helper()
	deps := &appsv1.DeploymentList{}
	require.NoError(t, e.cl.List(e.ctx, deps, ctrlruntimeclient.InNamespace(ns)))
	for _, d := range deps.Items {
		assert.NotContains(t, d.Annotations, restore.ReplicasAnnotation, d.Name)
		if d.Name == "kcp-operator" || d.Name == "platform-mesh-operator" {
			assert.Equal(t, int32(1), ptr.Deref(d.Spec.Replicas, 1), d.Name)
		}
	}
}

const providerKubeconfig = "bk-provider"

// populate makes a workspace on each shard with a marker, an org and a provider kubeconfig,
// and returns the state to compare after a restore.
func (e *env) populate(marker string) state {
	e.logf("populate kcp: a workspace on each shard with a marker, an org and a provider kubeconfig")
	e.createWorkspace("root", "bk-root", "root")
	e.createWorkspace("root", "bk-triton", "triton")
	for _, ws := range dataWorkspaces {
		cm := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "ConfigMap",
			"metadata": map[string]any{"name": "bk-marker"},
			"data":     map[string]any{"value": marker},
		}}
		_, err := e.kcp("root:"+ws).Resource(configMapGVR).Namespace("default").Create(e.ctx, cm, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			_, err = e.kcp("root:"+ws).Resource(configMapGVR).Namespace("default").Update(e.ctx, e.withRV("root:"+ws, cm), metav1.UpdateOptions{})
		}
		require.NoError(e.t, err)
	}
	e.createAccount("bk-org")
	e.createProviderKubeconfig(providerKubeconfig)
	e.eventually(5*time.Minute, "provider kubeconfig works", e.canList(e.providerCfg))
	return e.capture(dataWorkspaces, dataAccounts)
}

// prepareStorage creates a fresh BackupStorage and key, and removes Backups of earlier runs.
func (e *env) prepareStorage(dir string) cliStore {
	require.NoError(e.t, e.cl.DeleteAllOf(e.ctx, &pmbackupv1alpha1.Backup{}, ctrlruntimeclient.InNamespace(ns)))

	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	key := hex.EncodeToString(raw)
	store := cliStore{Prefix: "e2e-" + time.Now().Format("20060102-150405"), KeyFile: filepath.Join(dir, "backup.key")}
	require.NoError(e.t, os.WriteFile(store.KeyFile, []byte(key), 0o600))
	for name, data := range map[string]map[string]string{
		"e2e-backup-s3":  {"accessKeyID": s3AccessKey, "secretAccessKey": s3SecretKey},
		"e2e-backup-key": {"key": key},
	} {
		s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, StringData: data}
		_ = e.cl.Delete(e.ctx, s)
		require.NoError(e.t, e.cl.Create(e.ctx, s))
	}
	bs := &pmbackupv1alpha1.BackupStorage{
		ObjectMeta: metav1.ObjectMeta{Name: backupStore, Namespace: ns},
		Spec: pmbackupv1alpha1.BackupStorageSpec{
			Bucket:                 backupBucket,
			Prefix:                 store.Prefix,
			Endpoint:               "http://backup-s3:4566",
			Region:                 "us-east-1",
			ForcePathStyle:         true,
			CredentialsSecretRef:   corev1.LocalObjectReference{Name: "e2e-backup-s3"},
			EncryptionKeySecretRef: corev1.LocalObjectReference{Name: "e2e-backup-key"},
		},
	}
	_ = e.cl.Delete(e.ctx, bs)
	require.NoError(e.t, e.cl.Create(e.ctx, bs))
	e.logf("backup prefix %s", store.Prefix)
	return store
}

func (e *env) takeBackup(name string) *pmbackupv1alpha1.Backup {
	b := &pmbackupv1alpha1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       pmbackupv1alpha1.BackupSpec{StorageRef: corev1.LocalObjectReference{Name: backupStore}},
	}
	require.NoError(e.t, e.cl.Create(e.ctx, b))
	e.waitBackup(b)
	require.Equal(e.t, pmbackupv1alpha1.PhaseCompleted, b.Status.Phase, b.Status.Message)
	return b
}

func (e *env) waitBackup(b *pmbackupv1alpha1.Backup) {
	e.eventually(10*time.Minute, "backup "+b.Name+" finished", func() (string, error) {
		if err := e.cl.Get(e.ctx, ctrlruntimeclient.ObjectKeyFromObject(b), b); err != nil {
			return "", err
		}
		if b.Status.Phase.IsFinished() {
			return "", nil
		}
		return string(b.Status.Phase) + ": " + b.Status.Message, nil
	})
}

// loseSecrets deletes all kcp PKI secrets and waits until cert-manager made new ones.
func (e *env) loseSecrets() {
	bundle, err := pki.Collect(e.ctx, e.cl, ns)
	require.NoError(e.t, err)
	before := e.fingerprints()
	for _, s := range bundle.Secrets {
		require.NoError(e.t, ctrlruntimeclient.IgnoreNotFound(e.cl.Delete(e.ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: s.Name, Namespace: ns}})))
	}
	e.eventually(5*time.Minute, "cert-manager issued new secrets", func() (string, error) {
		changed := 0
		for name, fp := range before {
			now, err := e.fingerprint(name)
			if err != nil {
				return name + " not there yet", nil //nolint:nilerr // not there yet, poll again
			}
			if now != fp {
				changed++
			}
		}
		if changed < len(before) {
			return fmt.Sprintf("%d of %d changed", changed, len(before)), nil
		}
		for _, s := range bundle.Secrets {
			if err := e.cl.Get(e.ctx, ctrlruntimeclient.ObjectKeyFromObject(&s), &corev1.Secret{}); err != nil {
				return "secret " + s.Name + ": " + err.Error(), nil //nolint:nilerr // not there yet, poll again
			}
		}
		return "", nil
	})
	e.logf("%d kcp secrets deleted, cert-manager issued new keys", len(bundle.Secrets))
}

// loseCertificates deletes the kcp Certificates and the Issuers of kcp-operator.
func (e *env) loseCertificates() []string {
	bundle, err := pki.Collect(e.ctx, e.cl, ns)
	require.NoError(e.t, err)
	names := make([]string, 0, len(bundle.Certificates))
	for _, c := range bundle.Certificates {
		names = append(names, c.Name)
		require.NoError(e.t, ctrlruntimeclient.IgnoreNotFound(e.cl.Delete(e.ctx, &certmanagerv1.Certificate{ObjectMeta: metav1.ObjectMeta{Name: c.Name, Namespace: ns}})))
	}
	for _, i := range bundle.Issuers {
		require.NoError(e.t, ctrlruntimeclient.IgnoreNotFound(e.cl.Delete(e.ctx, &certmanagerv1.Issuer{ObjectMeta: metav1.ObjectMeta{Name: i.Name, Namespace: ns}})))
	}
	for _, k := range bundle.Kubeconfigs {
		require.NoError(e.t, ctrlruntimeclient.IgnoreNotFound(e.cl.Delete(e.ctx, k.DeepCopy())))
		secret, _, _ := unstructured.NestedString(k.Object, "spec", "secretRef", "name")
		require.NoError(e.t, ctrlruntimeclient.IgnoreNotFound(e.cl.Delete(e.ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secret, Namespace: ns}})))
	}
	e.logf("%d certificates, %d issuers and %d kubeconfigs with their secrets deleted", len(bundle.Certificates), len(bundle.Issuers), len(bundle.Kubeconfigs))
	return names
}

// verify checks that Platform Mesh works like before the data loss.
func (e *env) verify(t *testing.T, want state, label string) {
	t.Helper()
	e.waitHealthy(20 * time.Minute)
	e.eventually(20*time.Minute, "PlatformMesh ready", func() (string, error) {
		pm := &unstructured.Unstructured{}
		pm.SetAPIVersion("core.platform-mesh.io/v1alpha1")
		pm.SetKind("PlatformMesh")
		if err := e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: "platform-mesh"}, pm); err != nil {
			return "", err
		}
		conds, _, _ := unstructured.NestedSlice(pm.Object, "status", "conditions")
		for _, c := range conds {
			m, _ := c.(map[string]any)
			if m["type"] == "Ready" && m["status"] == "True" {
				return "", nil
			}
		}
		return "not ready", nil
	})

	// e.kcp uses the admin kubeconfig from before the data loss, so this also checks the old certificates
	var last []string
	e.eventually(5*time.Minute, "same data as before the backup", func() (string, error) {
		last = e.diff(want, dataWorkspaces, dataAccounts)
		return strings.Join(last, "; "), nil
	})
	assert.Empty(t, last)
	e.eventually(5*time.Minute, "provider kubeconfig from before the backup works", e.canList(e.providerCfg))
	e.eventually(5*time.Minute, "provider kubeconfig secret has the client certificate from before the backup", func() (string, error) {
		s := &corev1.Secret{}
		if err := e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: providerKubeconfig + "-kubeconfig"}, s); err != nil {
			if apierrors.IsNotFound(err) {
				return "missing", nil
			}
			return "", err
		}
		if len(s.Data["kubeconfig"]) == 0 {
			return "empty", nil
		}
		if !bytes.Equal(kcpConfig(e.t, s.Data["kubeconfig"]).CertData, e.providerCfg.CertData) {
			return "other client certificate", nil
		}
		return "", nil
	})

	for _, shard := range []string{"root", "triton"} {
		name := fmt.Sprintf("bk-check-%s-%s", label, shard)
		e.createWorkspace("root", name, shard)
		require.NoError(t, e.kcp("root").Resource(workspaceGVR).Delete(e.ctx, name, metav1.DeleteOptions{}))
	}
	account := "bk-check-" + label
	e.createAccount(account)
	require.NoError(t, e.kcp("root:orgs").Resource(accountGVR).Delete(e.ctx, account, metav1.DeleteOptions{}))
	e.logf("after %s restore: same data, same CA fingerprints, old admin and provider kubeconfigs work, new workspaces on both shards and a new org work", label)
}

func (e *env) withRV(path string, obj *unstructured.Unstructured) *unstructured.Unstructured {
	cur, err := e.kcp(path).Resource(configMapGVR).Namespace("default").Get(e.ctx, obj.GetName(), metav1.GetOptions{})
	require.NoError(e.t, err)
	obj.SetResourceVersion(cur.GetResourceVersion())
	return obj
}
