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

package e2e_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultE2ENamespace = "platform-mesh-system"
const backupOperatorDeployment = "backup-operator"

var (
	// cl is the test client — points at the host cluster where CRDs live.
	// WithWatch is required so the task simulator can open a watch stream on
	// EtcdOpsTask objects for immediate reaction rather than polling.
	cl client.WithWatch

	e2eNS      string
	testScheme = runtime.NewScheme()
)

func TestMain(m *testing.M) {
	e2eNS = os.Getenv("E2E_NAMESPACE")
	if e2eNS == "" {
		e2eNS = defaultE2ENamespace
	}

	if err := clientgoscheme.AddToScheme(testScheme); err != nil {
		panic(fmt.Sprintf("adding clientgo scheme: %v", err))
	}
	if err := backupv1alpha1.AddToScheme(testScheme); err != nil {
		panic(fmt.Sprintf("adding backup scheme: %v", err))
	}
	if err := druidv1alpha1.AddToScheme(testScheme); err != nil {
		panic(fmt.Sprintf("adding druid scheme: %v", err))
	}
	if err := coordinationv1.AddToScheme(testScheme); err != nil {
		panic(fmt.Sprintf("adding coordination scheme: %v", err))
	}

	// Host cluster client — all test objects (PlatformBackup, Etcd, etc.) and
	// operator preflight check both live on the host cluster.
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = home + "/.kube/config"
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(fmt.Sprintf("building kubeconfig from %s: %v", kubeconfig, err))
	}
	cl, err = client.NewWithWatch(cfg, client.Options{Scheme: testScheme})
	if err != nil {
		panic(fmt.Sprintf("creating client: %v", err))
	}

	if err := requireOperatorReady(); err != nil {
		fmt.Fprintf(os.Stderr, "\nPREFLIGHT FAILED: %v\n\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// requireOperatorReady verifies the backup-operator Deployment on the host cluster
// has at least one ready replica. Waits up to 3 minutes.
func requireOperatorReady() error {
	ctx := context.Background()
	nn := types.NamespacedName{Name: backupOperatorDeployment, Namespace: e2eNS}
	deadline := time.Now().Add(3 * time.Minute)

	for time.Now().Before(deadline) {
		var deploy appsv1.Deployment
		if err := cl.Get(ctx, nn, &deploy); err == nil && deploy.Status.ReadyReplicas >= 1 {
			return nil
		}
		fmt.Fprintf(os.Stderr, "waiting for deployment %s/%s...\n", e2eNS, backupOperatorDeployment)
		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf(
		"deployment %s/%s not ready after 3 minutes\n"+
			"  Deploy with: task deploy:kind\n"+
			"  Check logs:  kubectl logs -n %s deploy/%s",
		e2eNS, backupOperatorDeployment,
		e2eNS, backupOperatorDeployment,
	)
}

// cleanupTestResources deletes all e2e-prefixed Etcd, EtcdOpsTask, PlatformBackup,
// PlatformRestore, and full-snap coordination lease objects and waits for Etcd CRs
// to be gone (up to 30s). Call at the start of each test so leftover objects from
// prior tests don't contaminate the next test's shard discovery or task simulator.
func cleanupTestResources(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	var etcdList druidv1alpha1.EtcdList
	if err := cl.List(ctx, &etcdList, client.InNamespace(e2eNS)); err == nil {
		for i := range etcdList.Items {
			e := &etcdList.Items[i]
			if !strings.HasPrefix(e.Name, "e2e-") {
				continue
			}
			if len(e.Finalizers) > 0 {
				patch := client.MergeFrom(e.DeepCopy())
				e.Finalizers = nil
				_ = cl.Patch(ctx, e, patch)
			}
			_ = cl.Delete(ctx, e)
		}
	}

	// Delete any in-flight EtcdOpsTasks left by prior tests. Stale tasks cause the
	// task simulator or etcd-druid to process them for the wrong test context.
	var taskList druidv1alpha1.EtcdOpsTaskList
	if err := cl.List(ctx, &taskList, client.InNamespace(e2eNS)); err == nil {
		for i := range taskList.Items {
			task := &taskList.Items[i]
			if strings.HasPrefix(task.Name, "e2e-") {
				_ = cl.Delete(ctx, task)
			}
		}
	}

	var bkpList backupv1alpha1.PlatformBackupList
	if err := cl.List(ctx, &bkpList); err == nil {
		for i := range bkpList.Items {
			b := &bkpList.Items[i]
			if strings.HasPrefix(b.Name, "e2e-") {
				_ = cl.Delete(ctx, b)
			}
		}
	}

	var rstList backupv1alpha1.PlatformRestoreList
	if err := cl.List(ctx, &rstList); err == nil {
		for i := range rstList.Items {
			r := &rstList.Items[i]
			if strings.HasPrefix(r.Name, "e2e-") {
				_ = cl.Delete(ctx, r)
			}
		}
	}

	// Delete full-snap coordination leases left by prior tests. Stale leases cause
	// the baseline-key check to fire a false positive on the next test's first reconcile.
	var leaseList coordinationv1.LeaseList
	if err := cl.List(ctx, &leaseList, client.InNamespace(e2eNS)); err == nil {
		for i := range leaseList.Items {
			l := &leaseList.Items[i]
			if strings.HasPrefix(l.Name, "e2e-") {
				_ = cl.Delete(ctx, l)
			}
		}
	}

	// Wait for all e2e- Etcd CRs to disappear so the next test starts clean.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var remaining druidv1alpha1.EtcdList
		if err := cl.List(ctx, &remaining, client.InNamespace(e2eNS),
			client.MatchingLabels{backup.LabelKeyComponent: backup.LabelComponentKCPShard}); err != nil {
			break
		}
		found := false
		for _, e := range remaining.Items {
			if strings.HasPrefix(e.Name, "e2e-") {
				found = true
				break
			}
		}
		if !found {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func ptr32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

// suffix generates a short random string for unique resource names.
func suffix() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
