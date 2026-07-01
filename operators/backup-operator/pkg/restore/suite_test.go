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
	"os"
	"path/filepath"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const testNamespace = "default"

const etcdDruidFinalizer = "druid.gardener.cloud/etcd-druid"

func setupEnvtest(t *testing.T) (client.Client, *rest.Config, func()) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding clientgo scheme: %v", err)
	}
	if err := backupv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding backup scheme: %v", err)
	}
	if err := druidv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding druid scheme: %v", err)
	}
	if err := coordinationv1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding coordination scheme: %v", err)
	}
	if err := cnpgv1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding cnpg scheme: %v", err)
	}
	if err := velerov1.SchemeBuilder.AddToScheme(scheme); err != nil {
		t.Fatalf("adding velero scheme: %v", err)
	}

	assetsDir := os.Getenv("KUBEBUILDER_ASSETS")
	if assetsDir == "" {
		matches, _ := filepath.Glob(filepath.Join("..", "..", "bin", "k8s", "k8s", "*"))
		if len(matches) > 0 {
			assetsDir = matches[0]
		}
	}

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = filepath.Join(os.Getenv("HOME"), "go")
	}
	druidCRDDir := filepath.Join(gopath, "pkg", "mod",
		"github.com", "gardener", "etcd-druid", "api@v0.36.4",
		"core", "v1alpha1", "crds")

	env := &envtest.Environment{
		BinaryAssetsDirectory: assetsDir,
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd"),
			druidCRDDir,
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("starting envtest: %v", err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		_ = env.Stop()
		t.Fatalf("creating client: %v", err)
	}

	cleanupTestResources(t, c)
	waitForEtcdCleanup(t, c)

	return c, cfg, func() {
		cleanupTestResources(t, c)
		if err := env.Stop(); err != nil {
			t.Errorf("stopping envtest: %v", err)
		}
	}
}

func cleanupTestResources(t *testing.T, c client.Client) {
	t.Helper()
	ctx := t.Context()

	var etcdList druidv1alpha1.EtcdList
	if err := c.List(ctx, &etcdList, client.InNamespace(testNamespace)); err == nil {
		for i := range etcdList.Items {
			etcd := &etcdList.Items[i]
			if len(etcd.Finalizers) > 0 {
				patch := client.MergeFrom(etcd.DeepCopy())
				etcd.Finalizers = nil
				_ = c.Patch(ctx, etcd, patch)
			}
		}
	}

	_ = c.DeleteAllOf(ctx, &druidv1alpha1.EtcdOpsTask{}, client.InNamespace(testNamespace))
	_ = c.DeleteAllOf(ctx, &druidv1alpha1.Etcd{}, client.InNamespace(testNamespace))
	_ = c.DeleteAllOf(ctx, &backupv1alpha1.PlatformBackup{})
	_ = c.DeleteAllOf(ctx, &backupv1alpha1.PlatformRestore{})
	_ = c.DeleteAllOf(ctx, &coordinationv1.Lease{}, client.InNamespace(testNamespace))
}

func waitForEtcdCleanup(t *testing.T, c client.Client) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var etcdList druidv1alpha1.EtcdList
		if err := c.List(t.Context(), &etcdList, client.InNamespace(testNamespace)); err != nil {
			break
		}
		if len(etcdList.Items) == 0 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// runFinalizerCleaner watches for Etcd CRs being deleted and removes the etcd-druid finalizer.
func runFinalizerCleaner(ctx context.Context, cl client.Client) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			var etcdList druidv1alpha1.EtcdList
			if err := cl.List(ctx, &etcdList, client.InNamespace(testNamespace)); err != nil {
				continue
			}
			for i := range etcdList.Items {
				etcd := &etcdList.Items[i]
				if etcd.DeletionTimestamp.IsZero() {
					continue
				}
				hasFinalizer := false
				for _, f := range etcd.Finalizers {
					if f == etcdDruidFinalizer {
						hasFinalizer = true
						break
					}
				}
				if !hasFinalizer {
					continue
				}
				patch := client.MergeFrom(etcd.DeepCopy())
				newFinalizers := etcd.Finalizers[:0]
				for _, f := range etcd.Finalizers {
					if f != etcdDruidFinalizer {
						newFinalizers = append(newFinalizers, f)
					}
				}
				etcd.Finalizers = newFinalizers
				_ = cl.Patch(ctx, etcd, patch)
			}
		}
	}()
}

// runReadySimulator watches for non-ready Etcd CRs and sets ready=true.
func runReadySimulator(ctx context.Context, cl client.Client) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
			var etcdList druidv1alpha1.EtcdList
			if err := cl.List(ctx, &etcdList, client.InNamespace(testNamespace)); err != nil {
				continue
			}
			for i := range etcdList.Items {
				etcd := &etcdList.Items[i]
				if etcd.Status.Ready != nil && *etcd.Status.Ready {
					continue
				}
				patch := client.MergeFrom(etcd.DeepCopy())
				etcd.Status.Ready = ptr.To(true)
				_ = cl.Status().Patch(ctx, etcd, patch)
			}
		}
	}()
}
