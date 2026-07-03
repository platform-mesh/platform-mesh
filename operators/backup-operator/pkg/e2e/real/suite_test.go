//go:build e2e_real

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

package e2e_real_test

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultE2ENamespace      = "platform-mesh-backup-operator"
	backupOperatorDeployment = "backup-operator"
	etcdDruidDeployment      = "etcd-druid"
	etcdDruidNamespace       = "etcd-druid-system"
	// defaultLiveShardNamespace is the namespace where the real platform-mesh
	// Etcd CRs live. The sharded test discovers shard count from here.
	defaultLiveShardNamespace = "platform-mesh-system"
)

var (
	cl          ctrlruntimeclient.WithWatch
	e2eNS       string
	liveShardNS string
	restCfg     *rest.Config
	k8sClient   *kubernetes.Clientset
	testScheme  = runtime.NewScheme()
)

func TestMain(m *testing.M) {
	e2eNS = os.Getenv("E2E_NAMESPACE")
	if e2eNS == "" {
		e2eNS = defaultE2ENamespace
	}
	liveShardNS = os.Getenv("LIVE_SHARD_NAMESPACE")
	if liveShardNS == "" {
		liveShardNS = defaultLiveShardNamespace
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
	if err := appsv1.AddToScheme(testScheme); err != nil {
		panic(fmt.Sprintf("adding apps scheme: %v", err))
	}
	if err := batchv1.AddToScheme(testScheme); err != nil {
		panic(fmt.Sprintf("adding batch scheme: %v", err))
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = home + "/.kube/config"
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(fmt.Sprintf("building kubeconfig from %s: %v", kubeconfig, err))
	}
	restCfg = cfg
	k8sClient, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(fmt.Sprintf("creating kubernetes clientset: %v", err))
	}
	cl, err = ctrlruntimeclient.NewWithWatch(cfg, ctrlruntimeclient.Options{Scheme: testScheme})
	if err != nil {
		panic(fmt.Sprintf("creating client: %v", err))
	}

	ctx := context.Background()

	if err := requireEtcdDruidReady(); err != nil {
		fmt.Fprintf(os.Stderr, "\nPREFLIGHT FAILED (etcd-druid): %v\n\n", err)
		os.Exit(1)
	}
	if err := requireOperatorReady(); err != nil {
		fmt.Fprintf(os.Stderr, "\nPREFLIGHT FAILED (backup-operator): %v\n\n", err)
		os.Exit(1)
	}
	if err := requireNoPreexistingKCPShards(); err != nil {
		fmt.Fprintf(os.Stderr, "\nPREFLIGHT FAILED (pre-existing kcp-shard Etcd CRs): %v\n\n", err)
		os.Exit(1)
	}
	if err := EnsureMinioDeployed(ctx, cl, e2eNS); err != nil {
		fmt.Fprintf(os.Stderr, "\nPREFLIGHT FAILED (minio): %v\n\n", err)
		os.Exit(1)
	}

	// Clean up on SIGTERM/SIGINT so a task cancel or Ctrl-C leaves the cluster
	// tidy. SIGKILL cannot be caught — nothing we can do there.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\nreceived %s — running cleanup before exit\n", sig)
		cleanupE2EResources(context.Background())
		_ = EnsureMinioTeardown(context.Background(), cl, e2eNS)
		os.Exit(1)
	}()

	code := m.Run()

	cleanupE2EResources(ctx)
	if err := EnsureMinioTeardown(ctx, cl, e2eNS); err != nil {
		fmt.Fprintf(os.Stderr, "minio teardown error (non-fatal): %v\n", err)
	}

	os.Exit(code)
}

// cleanupE2EResources deletes every e2e-prefixed test resource left in the
// cluster. Called after m.Run() and on signal receipt so the cluster is always
// clean. Does not wait for Etcd CRs to fully terminate — use
// cleanupTestResources for per-test setup that needs to block until they're gone.
func cleanupE2EResources(ctx context.Context) {
	var etcdList druidv1alpha1.EtcdList
	if err := cl.List(ctx, &etcdList, ctrlruntimeclient.InNamespace(e2eNS)); err == nil {
		for i := range etcdList.Items {
			e := &etcdList.Items[i]
			if !strings.HasPrefix(e.Name, "e2e-") {
				continue
			}
			if len(e.Finalizers) > 0 {
				patch := ctrlruntimeclient.MergeFrom(e.DeepCopy())
				e.Finalizers = nil
				_ = cl.Patch(ctx, e, patch)
			}
			_ = cl.Delete(ctx, e)
		}
	}

	var taskList druidv1alpha1.EtcdOpsTaskList
	if err := cl.List(ctx, &taskList, ctrlruntimeclient.InNamespace(e2eNS)); err == nil {
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

	var leaseList coordinationv1.LeaseList
	if err := cl.List(ctx, &leaseList, ctrlruntimeclient.InNamespace(e2eNS)); err == nil {
		for i := range leaseList.Items {
			l := &leaseList.Items[i]
			if strings.HasPrefix(l.Name, "e2e-") {
				_ = cl.Delete(ctx, l)
			}
		}
	}

	// Delete etcdctl pods created by runEtcdctlPodOutput. These are corev1.Pods,
	// not Jobs — each test's t.Cleanup removes its own pod, but this path handles
	// pods left behind on SIGTERM or test panic (where t.Cleanup does not fire).
	var podList corev1.PodList
	if err := cl.List(ctx, &podList, ctrlruntimeclient.InNamespace(e2eNS),
		ctrlruntimeclient.MatchingLabels{"app": "etcdctl-e2e"}); err == nil {
		for i := range podList.Items {
			p := &podList.Items[i]
			_ = cl.Delete(ctx, p)
		}
	}
}

// cleanupTestResources calls cleanupE2EResources then waits up to 60s for all
// e2e-prefixed Etcd CRs to be fully deleted. Call at the start of each test to
// ensure no leftovers from a prior run interfere.
func cleanupTestResources(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	cleanupE2EResources(ctx)

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var remaining druidv1alpha1.EtcdList
		if err := cl.List(ctx, &remaining, ctrlruntimeclient.InNamespace(e2eNS),
			ctrlruntimeclient.MatchingLabels{backup.LabelKeyComponent: backup.LabelComponentKCPShard}); err != nil {
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
		time.Sleep(2 * time.Second)
	}
}

// requireEtcdDruidReady verifies the etcd-druid Deployment has at least one
// ready replica. Waits up to 2 minutes.
func requireEtcdDruidReady() error {
	ctx := context.Background()
	nn := types.NamespacedName{Name: etcdDruidDeployment, Namespace: etcdDruidNamespace}
	deadline := time.Now().Add(2 * time.Minute)

	for time.Now().Before(deadline) {
		var deploy appsv1.Deployment
		if err := cl.Get(ctx, nn, &deploy); err == nil && deploy.Status.ReadyReplicas >= 1 {
			return nil
		}
		fmt.Fprintf(os.Stderr, "waiting for deployment %s/%s...\n", etcdDruidNamespace, etcdDruidDeployment)
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf(
		"deployment %s/%s not ready after 2 minutes\n"+
			"  Deploy etcd-druid before running e2e_real tests",
		etcdDruidNamespace, etcdDruidDeployment,
	)
}

// requireOperatorReady verifies the backup-operator Deployment has at least one
// ready replica. Waits up to 3 minutes.
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

// requireNoPreexistingKCPShards fails if any Etcd CR in e2eNS already carries
// the kcp-shard label before the test creates its own. Such CRs would be
// discovered by PlatformBackup alongside the test shard, breaking the
// single-shard invariant.
func requireNoPreexistingKCPShards() error {
	ctx := context.Background()
	var list druidv1alpha1.EtcdList
	if err := cl.List(ctx, &list,
		ctrlruntimeclient.InNamespace(e2eNS),
		ctrlruntimeclient.MatchingLabels{backup.LabelKeyComponent: backup.LabelComponentKCPShard},
	); err != nil {
		return fmt.Errorf("listing Etcd CRs: %w", err)
	}
	if len(list.Items) > 0 {
		names := make([]string, len(list.Items))
		for i, e := range list.Items {
			names[i] = e.Name
		}
		return fmt.Errorf(
			"%d Etcd CR(s) in %s already carry label %s=%s: %v\n"+
				"  These would be included in every PlatformBackup and break the single-shard test.\n"+
				"  Remove the label or run in an isolated namespace (E2E_NAMESPACE=<other>).",
			len(list.Items), e2eNS,
			backup.LabelKeyComponent, backup.LabelComponentKCPShard, names,
		)
	}
	return nil
}
