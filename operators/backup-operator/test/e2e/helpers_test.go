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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/backup-operator/internal/testfixtures"
	"go.platform-mesh.io/backup-operator/pkg/druid"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ns           = "platform-mesh-system"
	kcpURL       = "https://kcp.api.portal.localhost:8443"
	etcdBucket   = "etcd-backups"
	backupBucket = "platform-backups"
	backupStore  = "e2e"
	// s3URL is the S3 store (LocalStack or MinIO) from this machine, the cluster uses http://backup-s3:4566.
	s3URL = "http://localhost:4566"
	// s3AccessKey and s3SecretKey are set by setup.sh. MinIO needs a secret of at least 8 characters.
	s3AccessKey = "backup-e2e"
	s3SecretKey = "backup-e2e-secret"
)

var (
	workspaceGVR = schema.GroupVersionResource{Group: "tenancy.kcp.io", Version: "v1alpha1", Resource: "workspaces"}
	accountGVR   = schema.GroupVersionResource{Group: "core.platform-mesh.io", Version: "v1alpha1", Resource: "accounts"}
	configMapGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

	// fingerprintSecrets are compared before and after a restore.
	fingerprintSecrets = []string{
		"root-ca", "root-client-ca", "root-server-ca", "root-requestheader-client-ca", "root-service-account-ca",
		"root-service-account", "triton-service-account", "cache-server-ca", "kcp-cluster-admin-client-cert",
	}
)

type env struct {
	t       *testing.T
	ctx     context.Context
	context string
	cl      ctrlruntimeclient.Client
	// kcpCfg is the kcp admin kubeconfig from before any data loss.
	kcpCfg *rest.Config
	// providerCfg is a kubeconfig of a Kubeconfig object made before the backup, nil when there is none.
	providerCfg *rest.Config
}

// newEnv uses the kcp admin kubeconfig of the cluster.
func newEnv(t *testing.T) *env {
	t.Helper()
	e := newEnvWithKcp(t, nil)
	s := &corev1.Secret{}
	require.NoError(t, e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: "kubeconfig-kcp-admin"}, s))
	e.kcpCfg = kcpConfig(t, s.Data["kubeconfig"])
	return e
}

func kcpConfig(t *testing.T, kubeconfig []byte) *rest.Config {
	t.Helper()
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	require.NoError(t, err)
	cfg.Host = kcpURL
	return cfg
}

// newEnvWithKcp uses a kcp admin kubeconfig saved before, for example from the cluster before a new install.
func newEnvWithKcp(t *testing.T, adminKubeconfig []byte) *env {
	t.Helper()
	kubeContext := os.Getenv("KUBE_CONTEXT")
	if kubeContext == "" {
		kubeContext = "kind-platform-mesh"
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: kubeContext},
	).ClientConfig()
	require.NoError(t, err)
	cl, err := ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: testfixtures.Scheme()})
	require.NoError(t, err)

	e := &env{t: t, ctx: context.Background(), context: kubeContext, cl: cl}
	if adminKubeconfig != nil {
		e.kcpCfg = kcpConfig(t, adminKubeconfig)
	}
	return e
}

func (e *env) logf(format string, args ...any) {
	e.t.Logf("%s "+format, append([]any{time.Now().Format("15:04:05")}, args...)...)
}

// kcp returns a client for one workspace path, for example root:orgs.
func (e *env) kcp(path string) dynamic.Interface {
	cfg := rest.CopyConfig(e.kcpCfg)
	cfg.Host = kcpURL + "/clusters/" + path
	c, err := dynamic.NewForConfig(cfg)
	require.NoError(e.t, err)
	return c
}

// eventually polls cond until it returns "" (done). The last message is shown on timeout.
func (e *env) eventually(timeout time.Duration, what string, cond func() (string, error)) {
	e.t.Helper()
	last := ""
	err := wait.PollUntilContextTimeout(e.ctx, 5*time.Second, timeout, true, func(context.Context) (bool, error) {
		msg, err := cond()
		if err != nil {
			last = err.Error()
			return false, nil //nolint:nilerr // errors are retried until the timeout
		}
		last = msg
		return msg == "", nil
	})
	require.NoError(e.t, err, "%s: %s", what, last)
}

func (e *env) createWorkspace(path, name, shard string) {
	ws := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tenancy.kcp.io/v1alpha1",
		"kind":       "Workspace",
		"metadata":   map[string]any{"name": name},
		"spec": map[string]any{
			"type":     map[string]any{"name": "universal", "path": "root"},
			"location": map[string]any{"selector": map[string]any{"matchLabels": map[string]any{"name": shard}}},
		},
	}}
	_, err := e.kcp(path).Resource(workspaceGVR).Create(e.ctx, ws, metav1.CreateOptions{})
	if !apierrors.IsAlreadyExists(err) {
		require.NoError(e.t, err)
	}
	e.eventually(5*time.Minute, "workspace "+name+" ready", func() (string, error) {
		ws, err := e.kcp(path).Resource(workspaceGVR).Get(e.ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		if phase, _, _ := unstructured.NestedString(ws.Object, "status", "phase"); phase != "Ready" {
			return "phase " + phase, nil
		}
		return "", nil
	})
}

func (e *env) createAccount(name string) {
	acc := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "core.platform-mesh.io/v1alpha1",
		"kind":       "Account",
		"metadata":   map[string]any{"name": name},
		"spec":       map[string]any{"type": "org", "displayName": name},
	}}
	// the webhook sets spec.creator to the request user, and the org invite needs it to be an email
	cfg := rest.CopyConfig(e.kcpCfg)
	cfg.Host = kcpURL + "/clusters/root:orgs"
	cfg.Impersonate = rest.ImpersonationConfig{UserName: "e2e@example.com", Groups: []string{"system:kcp:admin"}}
	orgs, err := dynamic.NewForConfig(cfg)
	require.NoError(e.t, err)
	_, err = orgs.Resource(accountGVR).Create(e.ctx, acc, metav1.CreateOptions{})
	if !apierrors.IsAlreadyExists(err) {
		require.NoError(e.t, err)
	}
	e.eventually(10*time.Minute, "account "+name+" ready", func() (string, error) {
		acc, err := e.kcp("root:orgs").Resource(accountGVR).Get(e.ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		conds, _, _ := unstructured.NestedSlice(acc.Object, "status", "conditions")
		for _, c := range conds {
			m, _ := c.(map[string]any)
			if m["type"] == "Ready" && m["status"] == "True" {
				return "", nil
			}
		}
		return "not ready", nil
	})
}

// state is what must be the same after a restore.
type state struct {
	Objects      map[string]string `json:"objects"`      // "kind path/name" -> uid
	Markers      map[string]string `json:"markers"`      // workspace -> marker value
	Fingerprints map[string]string `json:"fingerprints"` // secret -> sha256 of tls.crt
}

func (e *env) capture(workspaces []string, accounts []string) state {
	e.t.Helper()
	s := state{Objects: map[string]string{}, Markers: map[string]string{}, Fingerprints: map[string]string{}}
	for _, name := range workspaces {
		ws, err := e.kcp("root").Resource(workspaceGVR).Get(e.ctx, name, metav1.GetOptions{})
		require.NoError(e.t, err)
		cluster, _, _ := unstructured.NestedString(ws.Object, "spec", "cluster")
		s.Objects["workspace root/"+name] = string(ws.GetUID()) + " " + cluster
		cm, err := e.kcp("root:"+name).Resource(configMapGVR).Namespace("default").Get(e.ctx, "bk-marker", metav1.GetOptions{})
		require.NoError(e.t, err)
		s.Markers[name], _, _ = unstructured.NestedString(cm.Object, "data", "value")
	}
	for _, name := range accounts {
		acc, err := e.kcp("root:orgs").Resource(accountGVR).Get(e.ctx, name, metav1.GetOptions{})
		require.NoError(e.t, err)
		s.Objects["account root:orgs/"+name] = string(acc.GetUID())
	}
	s.Fingerprints = e.fingerprints()
	return s
}

func (e *env) fingerprints() map[string]string {
	out := map[string]string{}
	for _, name := range fingerprintSecrets {
		fp, err := e.fingerprint(name)
		require.NoError(e.t, err)
		out[name] = fp
	}
	return out
}

func (e *env) fingerprint(secret string) (string, error) {
	sec := &corev1.Secret{}
	if err := e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: secret}, sec); err != nil {
		return "", err
	}
	sum := sha256.Sum256(sec.Data["tls.crt"])
	return hex.EncodeToString(sum[:8]), nil
}

// diff lists everything that is missing or different compared to want.
func (e *env) diff(want state, workspaces, accounts []string) []string {
	var out []string
	got := state{Objects: map[string]string{}, Markers: map[string]string{}}
	for _, name := range workspaces {
		ws, err := e.kcp("root").Resource(workspaceGVR).Get(e.ctx, name, metav1.GetOptions{})
		if err != nil {
			out = append(out, fmt.Sprintf("workspace %s: %v", name, err))
			continue
		}
		cluster, _, _ := unstructured.NestedString(ws.Object, "spec", "cluster")
		got.Objects["workspace root/"+name] = string(ws.GetUID()) + " " + cluster
		cm, err := e.kcp("root:"+name).Resource(configMapGVR).Namespace("default").Get(e.ctx, "bk-marker", metav1.GetOptions{})
		if err != nil {
			out = append(out, fmt.Sprintf("marker in %s: %v", name, err))
			continue
		}
		got.Markers[name], _, _ = unstructured.NestedString(cm.Object, "data", "value")
	}
	for _, name := range accounts {
		acc, err := e.kcp("root:orgs").Resource(accountGVR).Get(e.ctx, name, metav1.GetOptions{})
		if err != nil {
			out = append(out, fmt.Sprintf("account %s: %v", name, err))
			continue
		}
		got.Objects["account root:orgs/"+name] = string(acc.GetUID())
	}
	for k, v := range want.Objects {
		if g, ok := got.Objects[k]; ok && g != v {
			out = append(out, fmt.Sprintf("%s: uid/cluster %q, want %q", k, g, v))
		}
	}
	for k, v := range want.Markers {
		if g, ok := got.Markers[k]; ok && g != v {
			out = append(out, fmt.Sprintf("marker in %s: %q, want %q", k, g, v))
		}
	}
	for k, v := range want.Fingerprints {
		if g, err := e.fingerprint(k); err != nil || g != v {
			out = append(out, fmt.Sprintf("secret %s: fingerprint %s (%v), want %s", k, g, err, v))
		}
	}
	return out
}

// waitHealthy waits until every Deployment and StatefulSet in the namespace is ready.
func (e *env) waitHealthy(timeout time.Duration) {
	e.eventually(timeout, "all deployments ready", func() (string, error) {
		deps := &appsv1.DeploymentList{}
		if err := e.cl.List(e.ctx, deps, ctrlruntimeclient.InNamespace(ns)); err != nil {
			return "", err
		}
		var notReady []string
		for _, d := range deps.Items {
			want := ptr.Deref(d.Spec.Replicas, 1)
			if d.Status.ReadyReplicas < want || d.Status.UpdatedReplicas < want {
				notReady = append(notReady, d.Name)
			}
		}
		sets := &appsv1.StatefulSetList{}
		if err := e.cl.List(e.ctx, sets, ctrlruntimeclient.InNamespace(ns)); err != nil {
			return "", err
		}
		for _, s := range sets.Items {
			if want := ptr.Deref(s.Spec.Replicas, 1); s.Status.ReadyReplicas < want {
				notReady = append(notReady, s.Name)
			}
		}
		if len(notReady) > 0 {
			return "not ready: " + strings.Join(notReady, ", "), nil
		}
		return "", nil
	})
}

// aws runs aws-cli next to the S3 store.
func (e *env) aws(args ...string) string {
	e.t.Helper()
	cmd := append([]string{"run", "--rm", "--network", "kind",
		"-e", "AWS_ACCESS_KEY_ID=" + s3AccessKey, "-e", "AWS_SECRET_ACCESS_KEY=" + s3SecretKey, "-e", "AWS_DEFAULT_REGION=us-east-1",
		"docker.io/amazon/aws-cli:2.27.0", "--endpoint-url", "http://backup-s3:4566"}, args...)
	out, err := exec.CommandContext(e.ctx, "docker", cmd...).CombinedOutput()
	require.NoError(e.t, err, "aws %v: %s", args, out)
	return string(out)
}

// loseEtcd wipes the data of every etcd and its druid store. Etcd starts again, empty.
func (e *env) loseEtcd() {
	etcds := &druidv1alpha1.EtcdList{}
	require.NoError(e.t, e.cl.List(e.ctx, etcds, ctrlruntimeclient.InNamespace(ns)))
	for i := range etcds.Items {
		require.NoError(e.t, druid.Scale(e.ctx, e.cl, &etcds.Items[i], 0))
	}
	for i := range etcds.Items {
		etcd := &etcds.Items[i]
		e.eventually(5*time.Minute, "etcd "+etcd.Name+" stopped", func() (string, error) {
			pods, err := druid.Pods(e.ctx, e.cl, etcd)
			if len(pods) > 0 {
				return "pods running", err
			}
			return "", err
		})
		pvcs, err := druid.PVCs(e.ctx, e.cl, etcd)
		require.NoError(e.t, err)
		for j := range pvcs {
			require.NoError(e.t, e.cl.Delete(e.ctx, &pvcs[j]))
		}
		e.aws("s3", "rm", "--recursive", "--quiet", fmt.Sprintf("s3://%s/%s/", etcdBucket, etcd.Spec.Backup.Store.Prefix))
	}
	for i := range etcds.Items {
		etcd := &etcds.Items[i]
		require.NoError(e.t, e.cl.Get(e.ctx, ctrlruntimeclient.ObjectKeyFromObject(etcd), etcd))
		require.NoError(e.t, druid.Scale(e.ctx, e.cl, etcd, 1))
	}
	e.logf("etcd data and druid store of %d etcd clusters deleted", len(etcds.Items))
}

func (e *env) scaleDeployment(name string, replicas int32) {
	d := &appsv1.Deployment{}
	require.NoError(e.t, e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: name}, d))
	patch := ctrlruntimeclient.MergeFrom(d.DeepCopy())
	d.Spec.Replicas = ptr.To(replicas)
	require.NoError(e.t, e.cl.Patch(e.ctx, d, patch))
	e.eventually(5*time.Minute, "deployment "+name+" scaled", func() (string, error) {
		if err := e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: name}, d); err != nil {
			return "", err
		}
		if d.Status.Replicas != replicas || d.Status.ReadyReplicas != replicas {
			return fmt.Sprintf("%d of %d", d.Status.ReadyReplicas, replicas), nil
		}
		return "", nil
	})
}
