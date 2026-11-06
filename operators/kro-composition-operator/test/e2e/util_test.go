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
	"embed"
	"fmt"
	"os"
	"testing"
	"time"

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/yaml"

	"github.com/kcp-dev/logicalcluster/v3"
	mcpclient "github.com/kcp-dev/multicluster-provider/client"
	"github.com/kcp-dev/multicluster-provider/envtest"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

//go:embed setup/*.yaml
var setupFixtures embed.FS

var (
	kcpConfig  *rest.Config
	kcpClient  mcpclient.ClusterClient
	testScheme *runtime.Scheme
)

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	env := &envtest.Environment{
		KcpStartTimeout:       2 * time.Minute,
		KcpStopTimeout:        30 * time.Second,
		BinaryAssetsDirectory: "../../../../bin", // TEST_KCP_ASSETS overrides
	}
	logf.SetLogger(zap.New(zap.UseDevMode(true)))

	var err error
	kcpConfig, err = env.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "starting kcp: %v\n", err)
		return 1
	}
	defer func() {
		if err := env.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "stopping kcp: %v\n", err)
		}
	}()

	testScheme = newTestScheme()
	kcpClient, err = mcpclient.New(kcpConfig, ctrlruntimeclient.Options{Scheme: testScheme})
	if err != nil {
		fmt.Fprintf(os.Stderr, "building kcp client: %v\n", err)
		return 1
	}
	return m.Run()
}

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		apiextensionsv1.AddToScheme,
		rbacv1.AddToScheme,
		corev1.AddToScheme,
		kcptenancyv1alpha1.AddToScheme,
		kcpcorev1alpha1.AddToScheme,
		kcpapisv1alpha1.AddToScheme,
		kcpapisv1alpha2.AddToScheme,
		krov1alpha1.AddToScheme,
	} {
		if err := add(s); err != nil {
			panic(err)
		}
	}
	return s
}

// consumer is a workspace with KROaaS "installed": the RGD + ContentConfiguration
// CRDs served, and a default namespace for the composed children.
type consumer struct {
	Path        logicalcluster.Path
	ClusterName string
	Client      ctrlruntimeclient.Client
}

// newConsumer creates a fresh workspace and installs everything the operator needs
// to act in it.
func newConsumer(tb testing.TB, name string) *consumer {
	tb.Helper()
	ws, path := envtest.NewWorkspaceFixture(tb, kcpClient, core.RootCluster.Path(), envtest.WithNamePrefix(name))
	cl := kcpClient.Cluster(path)
	c := &consumer{Path: path, ClusterName: ws.Spec.Cluster, Client: cl}

	applyCRD(tb, cl, "setup/kro.run_resourcegraphdefinitions.yaml")
	applyCRD(tb, cl, "setup/ui.platform-mesh.io_contentconfigurations.yaml")

	// the operator materializes composed children into the default namespace.
	mustCreate(tb, cl, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
	return c
}

func applyCRD(tb testing.TB, cl ctrlruntimeclient.Client, path string) {
	tb.Helper()
	raw, err := setupFixtures.ReadFile(path)
	require.NoError(tb, err, "read %s", path)
	crd := &apiextensionsv1.CustomResourceDefinition{}
	require.NoError(tb, yaml.Unmarshal(raw, crd), "decode %s", path)
	crd.ResourceVersion = ""
	mustCreate(tb, cl, crd)
	require.Eventually(tb, func() bool {
		got := &apiextensionsv1.CustomResourceDefinition{}
		if err := cl.Get(tb.Context(), ctrlruntimeclient.ObjectKeyFromObject(crd), got); err != nil {
			return false
		}
		for _, c := range got.Status.Conditions {
			if c.Type == apiextensionsv1.Established && c.Status == apiextensionsv1.ConditionTrue {
				return true
			}
		}
		return false
	}, 60*time.Second, time.Second, "CRD %s not established", crd.Name)
}

func mustCreate(tb testing.TB, cl ctrlruntimeclient.Client, obj ctrlruntimeclient.Object) {
	tb.Helper()
	err := cl.Create(tb.Context(), obj)
	if apierrors.IsAlreadyExists(err) {
		return // e.g. the workspace's pre-existing "default" namespace
	}
	require.NoError(tb, err, "create %T %s", obj, obj.GetName())
}
