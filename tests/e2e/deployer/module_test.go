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

package deployer

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmdeployv1alpha1 "go.platform-mesh.io/apis/deploy/v1alpha1"
	"go.platform-mesh.io/tests/e2e/deployer/suite"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	_ "embed"
)

//go:embed testdata/module/app.yaml
var moduleAppManifest string

const (
	moduleComponent = "github.com/platform-mesh/e2e-acme"
	moduleVersion   = "0.1.0"
	moduleNamespace = "acme-system"
	// The path the front proxy serves the module under.
	modulePath = "/services/acme/"

	// The workload reads this ConfigMap out of the module's kcp workspace.
	secretConfigMap = "module-secret"
	secretNamespace = "default"
)

// TestModule deploys a PlatformMesh, checks kcp works, installs a OCMModule from
// an in-cluster OCI registry, checks kcp still works and then checks the module
// itself is running with the context the deployer gave it.
func TestModule(t *testing.T) {
	t.Parallel()
	env := suite.Start(t, 0)
	env.EngageWorkload(t, "customer-a", env.Config, "rootshard", "frontproxy", "shards-default")
	env.InstallRegistry(t)
	image := env.BuildModuleApp(t)

	env.PublishComponent(t, suite.Component{
		Name:      moduleComponent,
		Version:   moduleVersion,
		Resources: map[string]string{"app-manifests": moduleAppManifest},
	})

	pm := createPlatformMesh(t, env.Config.Client, env.EtcdEndpoint())

	// The PlatformMesh must be up before a post-topology module deploys.
	waitPlatformMeshReady(t, env, pm.Name)
	env.VerifyKcp(t, env.Config, env.Config, 2)

	mod := acmeModule(env.RegistryURL(), image)
	require.NoError(t, env.Config.Client.Create(t.Context(), mod))

	// The deployer provisions root:modules:acme itself; seed a value that
	// is only reachable through that workspace.
	env.WaitWorkspace(t, env.Config, env.Config, "root:modules", "acme")
	want := seedSecret(t, env.WorkspaceClient(t, env.Config, env.Config, "root:modules:acme"))

	waitModuleReady(t, env, mod.Name)

	// Installing a module must not disturb kcp.
	env.VerifyKcp(t, env.Config, env.Config, 2)

	assertModuleRunning(t, env, mod, want)
}

// seedSecret writes a random value into the module's workspace and returns it.
func seedSecret(t *testing.T, ws ctrlruntimeclient.Client) string {
	t.Helper()
	value := rand.String(16)

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: secretNamespace}}
	if err := ws.Create(t.Context(), ns); err != nil && !apierrors.IsAlreadyExists(err) {
		require.NoError(t, err)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: secretConfigMap, Namespace: secretNamespace},
		Data:       map[string]string{"value": value},
	}
	if err := ws.Create(t.Context(), cm); err != nil {
		require.True(t, apierrors.IsAlreadyExists(err), "creating ConfigMap: %v", err)
		require.NoError(t, ws.Update(t.Context(), cm))
	}
	t.Logf("seeded %s/%s in the module workspace with %q", secretNamespace, secretConfigMap, value)
	return value
}

func acmeModule(registry, image string) *pmdeployv1alpha1.OCMModule {
	values := fmt.Sprintf(
		`{"replicas":1,"image":%q,"greeting":"hello","secretNamespace":%q,"secretName":%q}`,
		image, secretNamespace, secretConfigMap)

	return &pmdeployv1alpha1.OCMModule{
		ObjectMeta: metav1.ObjectMeta{Name: "acme", Namespace: suite.ProviderNamespace},
		Spec: pmdeployv1alpha1.OCMModuleSpec{
			PlatformMeshRef: corev1.LocalObjectReference{Name: "customer-a"},
			Stage:           pmdeployv1alpha1.StagePostTopology,
			OCM:             &pmdeployv1alpha1.OCMRepository{URL: registry},
			Component:       moduleComponent,
			Version:         moduleVersion,
			Values:          &apiextensionsv1.JSON{Raw: []byte(values)},
			Workspaces: []pmdeployv1alpha1.OCMModuleWorkspace{{
				Name: "",
			}},
			Kubeconfigs: []pmdeployv1alpha1.OCMModuleKubeconfig{{
				Name:   "kcp",
				Target: pmdeployv1alpha1.KubeconfigTargetFrontProxy,
			}},
			Components: []pmdeployv1alpha1.OCMModuleComponent{{
				Name:        "app",
				Resource:    "app-manifests",
				Placement:   pmdeployv1alpha1.PlacementPerFrontProxy,
				Namespace:   moduleNamespace,
				Kubeconfigs: []string{"kcp"},
				Mapping: &pmdeployv1alpha1.Mapping{
					Path:    modulePath,
					Service: `${module + "-" + component}`,
					Port:    8443,
				},
			}},
		},
	}
}

func waitPlatformMeshReady(t *testing.T, env *suite.Env, name string) {
	t.Helper()
	key := ctrlruntimeclient.ObjectKey{Namespace: suite.ProviderNamespace, Name: name}
	require.Eventually(t, func() bool {
		pm := &pmdeployv1alpha1.PlatformMesh{}
		if err := env.Config.Client.Get(t.Context(), key, pm); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(pm.Status.Conditions, "Ready")
	}, 10*time.Minute, 5*time.Second, "PlatformMesh %q did not become ready", name)
}

func waitModuleReady(t *testing.T, env *suite.Env, name string) {
	t.Helper()
	key := ctrlruntimeclient.ObjectKey{Namespace: suite.ProviderNamespace, Name: name}
	require.Eventually(t, func() bool {
		mod := &pmdeployv1alpha1.OCMModule{}
		if err := env.Config.Client.Get(t.Context(), key, mod); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(mod.Status.Conditions, "Ready")
	}, 5*time.Minute, 5*time.Second, "OCMModule %q did not become ready", name)
}

// assertModuleRunning checks the objects the deployer applied and that the
// workload reports the context it was given.
func assertModuleRunning(t *testing.T, env *suite.Env, mod *pmdeployv1alpha1.OCMModule, wantSecret string) {
	t.Helper()
	cl := env.Config.Client
	clusterID := env.Config.NodeIP

	// The generated ConfigMap carries the instance's facts.
	cm := &corev1.ConfigMap{}
	require.NoError(t, cl.Get(t.Context(),
		ctrlruntimeclient.ObjectKey{Namespace: moduleNamespace, Name: "acme-app"}, cm))
	assert.Equal(t, "acme", cm.Data["MODULE"])
	assert.Equal(t, "app", cm.Data["COMPONENT"])
	assert.Equal(t, "per-front-proxy", cm.Data["PLACEMENT"])
	assert.Equal(t, clusterID, cm.Data["CLUSTER"])
	assert.Equal(t, "customer-a", cm.Data["PLATFORM_MESH"])

	// The templated Deployment rolled out.
	dep := &appsv1.Deployment{}
	require.Eventually(t, func() bool {
		if err := cl.Get(t.Context(),
			ctrlruntimeclient.ObjectKey{Namespace: moduleNamespace, Name: "acme-app"}, dep); err != nil {
			return false
		}
		return dep.Status.ReadyReplicas > 0
	}, 5*time.Minute, 5*time.Second, "module Deployment did not become ready")
	assert.Equal(t, "acme", dep.Labels["deploy.platform-mesh.io/ocmmodule"])
	// The payload read these off its own component descriptor.
	assert.Equal(t, moduleVersion, dep.Labels["e2e.platform-mesh.io/component-version"])
	assert.Equal(t, suite.ManifestResourceType, dep.Annotations["e2e.platform-mesh.io/payload-type"])

	// The workload answers with what the deployer handed it, which proves
	// the ConfigMap and the templated value both arrived.
	body := getModuleIdentity(t, env)
	assert.Equal(t, "acme", body["module"])
	assert.Equal(t, "app", body["component"])
	assert.Equal(t, "per-front-proxy", body["placement"])
	assert.Equal(t, clusterID, body["cluster"])
	assert.Equal(t, "customer-a", body["platformMesh"])
	assert.Equal(t, "hello", body["greeting"])
	assert.Equal(t, "root:modules:acme", body["workspace"])

	// The value only exists inside the module's kcp workspace, so reading
	// it proves the minted kubeconfig and its cluster-admin binding work.
	assert.Empty(t, body["error"], "module could not read its workspace")
	assert.Equal(t, wantSecret, body["secret"])

	// Status records the fan-out.
	require.NoError(t, cl.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(mod), mod))
	require.Len(t, mod.Status.Components, 1)
	assert.Equal(t, "app", mod.Status.Components[0].Name)
	require.Len(t, mod.Status.Components[0].Instances, 1)
	assert.Equal(t, clusterID, mod.Status.Components[0].Instances[0].Cluster)
}

// getModuleIdentity fetches the workload through the front proxy, which is the
// point of the mapping: the module is reachable on the kcp endpoint, not on a
// port of its own.
func getModuleIdentity(t *testing.T, env *suite.Env) map[string]string {
	t.Helper()

	url := "https://fp." + env.Config.NodeIP + ".sslip.io:31443" + modulePath

	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			// The front proxy serves the kcp PKI, which the test does
			// not carry; the assertion is about routing and the body.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test
			DialContext:     suite.FrontProxyDialer(t, env.Config),
		},
	}

	var out map[string]string
	var lastErr string
	require.Eventuallyf(t, func() bool {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if err != nil {
			lastErr = err.Error()
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err.Error()
			return false
		}
		defer resp.Body.Close() //nolint:errcheck // test
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err.Error()
			return false
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Sprintf("status %d: %s", resp.StatusCode, body)
			return false
		}
		if err := json.Unmarshal(body, &out); err != nil {
			lastErr = fmt.Sprintf("body %q: %v", body, err)
			return false
		}
		return true
	}, 5*time.Minute, 5*time.Second, "module not reachable through the front proxy on %s: %s", url, lateString{&lastErr})
	return out
}

// lateString reads the value when testify renders the failure message rather
// than when Eventuallyf is called.
type lateString struct{ s *string }

func (l lateString) String() string { return *l.s }
