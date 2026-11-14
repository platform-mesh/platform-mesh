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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	buildOnce sync.Once
	cliBin    string
	buildErr  error
)

// cliStore is the bucket, prefix and key file that the backup and restore commands use.
type cliStore struct {
	Prefix  string `json:"prefix"`
	KeyFile string `json:"keyFile"`
}

// cli runs the backup-operator binary built from this repository.
func (e *env) cli(store cliStore, args ...string) (string, error) {
	buildOnce.Do(func() {
		cliBin = filepath.Join(os.TempDir(), fmt.Sprintf("backup-operator-e2e-%d", os.Getpid()))
		out, err := exec.Command("go", "build", "-o", cliBin, "../..").CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("go build: %w: %s", err, out)
		}
	})
	require.NoError(e.t, buildErr)

	args = append(args, "--context", e.context, "--bucket", backupBucket, "--prefix", store.Prefix,
		"--endpoint", s3URL, "--path-style", "--key-file", store.KeyFile, "--etcd-store-endpoint", s3URL)
	cmd := exec.CommandContext(e.ctx, cliBin, args...)
	cmd.Env = append(os.Environ(), "AWS_ACCESS_KEY_ID="+s3AccessKey, "AWS_SECRET_ACCESS_KEY="+s3SecretKey)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	e.logf("backup-operator %s %s: %v after %s\n%s", args[0], args[2], err, time.Since(start).Round(time.Second), out)
	return string(out), err
}

func (e *env) cliBackup(store cliStore, name string) {
	_, err := e.cli(store, "backup", "--name", name)
	require.NoError(e.t, err)
}

func (e *env) cliRestore(store cliStore, name string, parts ...string) error {
	args := []string{"restore", "--name", name}
	if len(parts) > 0 {
		args = append(args, "--only", strings.Join(parts, ","))
	}
	_, err := e.cli(store, args...)
	return err
}

// createProviderKubeconfig mints a kubeconfig with a Kubeconfig object, like a platform does for
// its operators. It returns the kubeconfig.
func (e *env) createProviderKubeconfig(name string) []byte {
	kc := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "operator.kcp.io/v1alpha1",
		"kind":       "Kubeconfig",
		"metadata":   map[string]any{"name": name, "namespace": ns},
		"spec": map[string]any{
			"username":  name,
			"groups":    []any{"system:kcp:admin"},
			"validity":  "8766h",
			"secretRef": map[string]any{"name": name + "-kubeconfig"},
			"target":    map[string]any{"frontProxyRef": map[string]any{"name": "frontproxy"}},
		},
	}}
	err := e.cl.Create(e.ctx, kc)
	if err != nil {
		require.NoError(e.t, ctrlruntimeclient.IgnoreAlreadyExists(err))
	}
	var data []byte
	e.eventually(5*time.Minute, "kubeconfig "+name, func() (string, error) {
		s := &corev1.Secret{}
		if err := e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: name + "-kubeconfig"}, s); err != nil {
			return "", err
		}
		data = s.Data["kubeconfig"]
		if len(data) == 0 {
			return "no kubeconfig yet", nil
		}
		return "", nil
	})
	e.providerCfg = kcpConfig(e.t, data)
	return data
}

// canList is "" when the kubeconfig can list the workspaces in root.
func (e *env) canList(cfg *rest.Config) func() (string, error) {
	return func() (string, error) {
		c := rest.CopyConfig(cfg)
		c.Host = kcpURL + "/clusters/root"
		cl, err := dynamic.NewForConfig(c)
		if err != nil {
			return "", err
		}
		_, err = cl.Resource(workspaceGVR).List(e.ctx, metav1.ListOptions{Limit: 1})
		return "", err
	}
}
