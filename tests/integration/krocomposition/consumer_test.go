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

package krocomposition

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/kro-composition-operator/pkg/app"
	"go.platform-mesh.io/platform-mesh/tests/framework/fixtures"
	"go.platform-mesh.io/platform-mesh/tests/framework/kcp"
	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	kcptc "github.com/kcp-dev/kcp-testcontainers"
	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

const kroModule = "github.com/kubernetes-sigs/kro"

// testScheme is what the tests' clients and the engine use: the framework's
// kcp scheme plus kro's API.
func testScheme(tb testing.TB) *runtime.Scheme {
	tb.Helper()
	return scheme.New(tb, krov1alpha1.AddToScheme)
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
	env, err := kcp.Default(tb.Context())
	require.NoError(tb, err)
	path, err := env.CreateWorkspace(tb.Context(), core.RootCluster.Path().Join(name+"-").String(), kcptc.WaitForReachable())
	require.NoError(tb, err)
	cl, err := env.Client(path.String(), ctrlruntimeclient.Options{Scheme: testScheme(tb)})
	require.NoError(tb, err)
	lc := &kcpcorev1alpha1.LogicalCluster{}
	require.NoError(tb, cl.Get(tb.Context(), ctrlruntimeclient.ObjectKey{Name: kcpcorev1alpha1.LogicalClusterName}, lc))
	c := &consumer{Path: path, ClusterName: logicalcluster.From(lc).String(), Client: cl}

	// The RGD CRD as kro ships it, at the version the operator is built against.
	applyCRD(tb, cl, filepath.Join(moduleDir(tb, kroModule), "helm", "crds", "kro.run_resourcegraphdefinitions.yaml"))
	// The ContentConfiguration CRD (go.platform-mesh.io/apis/ui/v1alpha1), as
	// extension-manager-operator generates and ships it.
	applyCRD(tb, cl, filepath.Join(repoRoot(tb), "operators", "extension-manager-operator", "config", "crd", "bases", "ui.platform-mesh.io_contentconfigurations.yaml"))

	// the operator materializes composed children into the default namespace.
	fixtures.Apply(tb, cl, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
	return c
}

// newEngine returns the operator's engine reaching every workspace through the
// kcp front-proxy; its per-workspace controllers stop with the test.
func newEngine(t *testing.T) *app.Engine {
	t.Helper()
	env, err := kcp.Default(t.Context())
	require.NoError(t, err)
	return app.NewEngine(t.Context(), env.BaseConfig(), testScheme(t), dcConfig())
}

func applyCRD(tb testing.TB, cl ctrlruntimeclient.Client, path string) {
	tb.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(tb, err, "read %s", path)
	crd := &apiextensionsv1.CustomResourceDefinition{}
	require.NoError(tb, yaml.Unmarshal(raw, crd), "decode %s", path)
	crd.ResourceVersion = ""
	fixtures.Apply(tb, cl, crd)
	wait.CRDEstablished(tb, cl, crd.Name)
}

// moduleDir returns the directory of module as the root go.mod resolves it,
// so the files it ships match the code compiled in.
func moduleDir(tb testing.TB, module string) string {
	tb.Helper()
	out, err := exec.CommandContext(tb.Context(), "go", "list", "-m", "-f", "{{.Dir}}", module).Output()
	require.NoError(tb, err, "go list -m %s", module)
	dir := strings.TrimSpace(string(out))
	require.NotEmpty(tb, dir, "module %s is not downloaded", module)
	return dir
}

// repoRoot is the platform-mesh repository root: go test runs a package in its
// source directory, tests/integration/krocomposition.
func repoRoot(tb testing.TB) string {
	tb.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(tb, err, "resolving the repository root")
	return root
}
