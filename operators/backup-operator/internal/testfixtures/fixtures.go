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

// Package testfixtures builds a fake runtime cluster with kcp, druid and cert-manager objects for unit tests.
package testfixtures

import (
	"context"
	"fmt"
	"strings"
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/storage"
	"go.platform-mesh.io/subroutines"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	Namespace   = "platform-mesh-system"
	Storage     = "s3"
	EtcdBucket  = "etcd-backups"
	BackupBkt   = "platform-backups"
	KcpOperator = "kcp-operator"
	KcpClient   = "security-operator"
)

// Key is a valid 32 byte encryption key.
var Key = []byte("0123456789abcdef0123456789abcdef")

// Etcds are the etcd clusters of the fake cluster.
var Etcds = []string{"etcd-cache", "etcd-kcp", "etcd-kcp-triton"}

func Scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = pmbackupv1alpha1.AddToScheme(s)
	_ = druidv1alpha1.AddToScheme(s)
	_ = certmanagerv1.AddToScheme(s)
	for _, kind := range []string{"RootShard", "Shard", "CacheServer", "FrontProxy", "Kubeconfig"} {
		gv := schema.GroupVersion{Group: "operator.kcp.io", Version: "v1alpha1"}
		s.AddKnownTypeWithName(gv.WithKind(kind), &unstructured.Unstructured{})
		s.AddKnownTypeWithName(gv.WithKind(kind+"List"), &unstructured.UnstructuredList{})
	}
	return s
}

// Cluster returns a fake client with kcp (root shard, triton shard, cache server, front-proxy),
// three ready etcd clusters with a druid store, the kcp PKI and the BackupStorage.
func Cluster(t *testing.T, extra ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	t.Helper()
	objs := make([]ctrlruntimeclient.Object, 0, 32+len(extra))
	objs = append(objs,
		secret("s3-credentials", map[string]string{"accessKeyID": "a", "secretAccessKey": "b"}),
		secret("backup-key", map[string]string{"key": string(Key)}),
		&pmbackupv1alpha1.BackupStorage{
			ObjectMeta: metav1.ObjectMeta{Name: Storage, Namespace: Namespace},
			Spec: pmbackupv1alpha1.BackupStorageSpec{
				Bucket:                 BackupBkt,
				Prefix:                 "backups",
				CredentialsSecretRef:   corev1.LocalObjectReference{Name: "s3-credentials"},
				EncryptionKeySecretRef: corev1.LocalObjectReference{Name: "backup-key"},
			},
		},
		deployment(KcpOperator, 1, nil),
	)
	owners := map[string]*unstructured.Unstructured{}
	for kind, name := range map[string]string{"RootShard": "root", "Shard": "triton", "CacheServer": "cache-server", "FrontProxy": "frontproxy"} {
		u := kcpObject(kind, name)
		owners[kind] = u
		objs = append(objs, u)
	}
	for _, name := range Etcds {
		objs = append(objs, Etcd(name, 1, true))
	}
	objs = append(objs,
		withSecretVolume(deployment("root-kcp", 1, owners["RootShard"]), "root-kcp-kubeconfig"),
		deployment("triton-shard-kcp", 1, owners["Shard"]),
		deployment("frontproxy-front-proxy", 2, owners["FrontProxy"]),
		withSecretVolume(deployment(KcpClient, 1, nil), KcpClient+"-kubeconfig"),
		withSecretVolume(deployment("openfga", 1, nil), "openfga-tls"),
	)
	objs = append(objs, PKI(owners)...)
	objs = append(objs, kubeconfig(Kubeconfig, owners["FrontProxy"]))
	objs = append(objs, extra...)

	return fake.NewClientBuilder().
		WithScheme(Scheme()).
		WithObjects(objs...).
		WithStatusSubresource(&pmbackupv1alpha1.Backup{}, &druidv1alpha1.Etcd{}, &druidv1alpha1.EtcdOpsTask{}).
		Build()
}

func kcpObject(kind, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "operator.kcp.io", Version: "v1alpha1", Kind: kind})
	u.SetNamespace(Namespace)
	u.SetName(name)
	u.SetUID(types.UID("uid-" + name))
	_ = unstructured.SetNestedField(u.Object, "Running", "status", "phase")
	return u
}

// Kubeconfig is the kcp-operator Kubeconfig of the fake cluster.
const Kubeconfig = "provider"

func kubeconfig(name string, frontProxy *unstructured.Unstructured) *unstructured.Unstructured {
	u := kcpObject("Kubeconfig", name)
	_ = unstructured.SetNestedField(u.Object, "kubeconfig-cert-"+name, "spec", "secretRef", "name")
	_ = unstructured.SetNestedField(u.Object, frontProxy.GetName(), "spec", "target", "frontProxyRef", "name")
	return u
}

func ownerRef(u *unstructured.Unstructured) []metav1.OwnerReference {
	if u == nil {
		return nil
	}
	return []metav1.OwnerReference{{
		APIVersion: "operator.kcp.io/v1alpha1", Kind: u.GetKind(), Name: u.GetName(), UID: u.GetUID(), Controller: ptr.To(true),
	}}
}

func secret(name string, data map[string]string) *corev1.Secret {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: Namespace}, Data: map[string][]byte{}}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

func deployment(name string, replicas int32, owner *unstructured.Unstructured) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: Namespace, OwnerReferences: ownerRef(owner)},
		Spec:       appsv1.DeploymentSpec{Replicas: ptr.To(replicas)},
		Status:     appsv1.DeploymentStatus{Replicas: replicas, ReadyReplicas: replicas, UpdatedReplicas: replicas},
	}
}

func withSecretVolume(d *appsv1.Deployment, secretName string) *appsv1.Deployment {
	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: secretName, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName}},
	})
	return d
}

// StorePrefix is the druid store prefix of an etcd. No prefix starts with another one.
func StorePrefix(etcd string) string { return etcd + "-store" }

// Etcd returns a druid Etcd with an S3 store under StorePrefix.
func Etcd(name string, replicas int32, ready bool) *druidv1alpha1.Etcd {
	return &druidv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: Namespace},
		Spec: druidv1alpha1.EtcdSpec{
			Replicas: replicas,
			Backup: druidv1alpha1.BackupSpec{Store: &druidv1alpha1.StoreSpec{
				Container: ptr.To(EtcdBucket),
				Prefix:    StorePrefix(name),
				Provider:  ptr.To[druidv1alpha1.StorageProvider]("S3"),
				SecretRef: &corev1.SecretReference{Name: "etcd-backup-s3"},
			}},
		},
		Status: druidv1alpha1.EtcdStatus{Ready: ptr.To(ready), ReadyReplicas: replicas},
	}
}

// PKI returns a small kcp PKI: a self-signed root CA, a kcp CA issuer, a leaf owned by the
// RootShard, a leaf from the chart signed by the kcp CA, and one unrelated certificate.
func PKI(owners map[string]*unstructured.Unstructured) []ctrlruntimeclient.Object {
	root := owners["RootShard"]
	cert := func(name, issuer string, owner *unstructured.Unstructured) *certmanagerv1.Certificate {
		return &certmanagerv1.Certificate{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: Namespace, OwnerReferences: ownerRef(owner)},
			Spec: certmanagerv1.CertificateSpec{
				SecretName: name,
				IssuerRef:  cmmeta.IssuerReference{Name: issuer, Kind: "Issuer"},
			},
		}
	}
	tlsSecret := func(name string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: Namespace, Annotations: map[string]string{"cert-manager.io/certificate-name": name}},
			Type:       corev1.SecretTypeTLS,
			Data:       map[string][]byte{"tls.crt": []byte("crt-" + name), "tls.key": []byte("key-" + name)},
		}
	}
	return []ctrlruntimeclient.Object{
		&certmanagerv1.Issuer{ObjectMeta: metav1.ObjectMeta{Name: "selfsigned", Namespace: Namespace},
			Spec: certmanagerv1.IssuerSpec{IssuerConfig: certmanagerv1.IssuerConfig{SelfSigned: &certmanagerv1.SelfSignedIssuer{}}}},
		&certmanagerv1.Issuer{ObjectMeta: metav1.ObjectMeta{Name: "root-ca", Namespace: Namespace, OwnerReferences: ownerRef(root)},
			Spec: certmanagerv1.IssuerSpec{IssuerConfig: certmanagerv1.IssuerConfig{CA: &certmanagerv1.CAIssuer{SecretName: "root-ca"}}}},
		&certmanagerv1.Issuer{ObjectMeta: metav1.ObjectMeta{Name: "other-issuer", Namespace: Namespace}},
		cert("root-ca", "selfsigned", root), tlsSecret("root-ca"),
		cert("root-server", "root-ca", root), tlsSecret("root-server"),
		cert("kcp-cluster-admin-client-cert", "root-ca", nil), tlsSecret("kcp-cluster-admin-client-cert"),
		cert("webhook-cert", "other-issuer", nil), tlsSecret("webhook-cert"),
		&certmanagerv1.CertificateRequest{ObjectMeta: metav1.ObjectMeta{Name: "root-ca-2", Namespace: Namespace,
			Annotations: map[string]string{"cert-manager.io/certificate-name": "root-ca"}}},
		&certmanagerv1.CertificateRequest{ObjectMeta: metav1.ObjectMeta{Name: "webhook-cert-1", Namespace: Namespace,
			Annotations: map[string]string{"cert-manager.io/certificate-name": "webhook-cert"}}},
	}
}

// KcpPKISecrets are the secrets that must be in a backup of the fake cluster.
var KcpPKISecrets = []string{"kcp-cluster-admin-client-cert", "root-ca", "root-server"}

// Run calls the subroutines like the lifecycle does, once. It returns the result that ended the chain.
func Run(ctx context.Context, t *testing.T, subs []subroutines.Subroutine, obj ctrlruntimeclient.Object) (subroutines.Result, string) {
	t.Helper()
	for _, s := range subs {
		res, err := s.(subroutines.Processor).Process(ctx, obj)
		require.NoError(t, err, "subroutine %s", s.GetName())
		if res.IsStop() || res.IsStopWithRequeue() || res.IsSkipAll() {
			return res, s.GetName()
		}
	}
	return subroutines.OK(), ""
}

// SimulateDruid marks all EtcdOpsTasks done and writes a full snapshot for each into the druid store.
func SimulateDruid(ctx context.Context, t *testing.T, cl ctrlruntimeclient.Client, stores *storage.MemoryFactory, rev int) {
	t.Helper()
	tasks := &druidv1alpha1.EtcdOpsTaskList{}
	require.NoError(t, cl.List(ctx, tasks))
	for i := range tasks.Items {
		task := &tasks.Items[i]
		if task.Status.State != nil {
			continue
		}
		task.Status.State = ptr.To(druidv1alpha1.TaskStateSucceeded)
		require.NoError(t, cl.Status().Update(ctx, task))
		// the fake client does not set creationTimestamp, then any snapshot time is newer
		taken := max(task.CreationTimestamp.Unix(), 0) + 1
		key := fmt.Sprintf("%s/v2/Full-00000000-%08d-%d", StorePrefix(*task.Spec.EtcdName), rev, taken)
		require.NoError(t, storage.PutBytes(ctx, stores.Bucket(EtcdBucket), key, []byte("snapshot "+*task.Spec.EtcdName+fmt.Sprint(rev))))
	}
}

// KeysWithPrefix returns the keys of a memory bucket with the prefix.
func KeysWithPrefix(m *storage.Memory, prefix string) []string {
	var out []string
	for _, k := range m.Keys() {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out
}
