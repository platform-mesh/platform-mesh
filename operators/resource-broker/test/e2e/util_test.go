//go:build kube_legacy

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
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	mctrl "sigs.k8s.io/multicluster-runtime"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	"sigs.k8s.io/multicluster-runtime/providers/multi"
	"sigs.k8s.io/multicluster-runtime/providers/single"

	brokerv1alpha1 "go.platform-mesh.io/apis/operatorbroker/v1alpha1"
	examplev1alpha1 "go.platform-mesh.io/resource-broker/api/example/v1alpha1"
	"go.platform-mesh.io/resource-broker/test/e2e/internal/manager"
)

func init() {
	log.SetLogger(zap.New(zap.UseDevMode(true)))
	// TODO manage schemes properly
	runtime.Must(examplev1alpha1.AddToScheme(scheme.Scheme))
	runtime.Must(brokerv1alpha1.AddToScheme(scheme.Scheme))
}

// ManagerOptions returns the default manager options for tests.
func ManagerOptions() mctrl.Options {
	return mctrl.Options{
		LeaderElectionID: rand.Text(),
		Metrics: metricsserver.Options{
			BindAddress: "0", // disable metrics
		},
		Controller: ctrlconfig.Controller{
			SkipNameValidation: ptr.To(true), // skip name validation of controller metrics for tests
		},
	}
}

type Frame struct {
	Coordination *ControlPlane
	Compute      *ControlPlane
	Consumers    map[string]*ControlPlane
	cProvider    *multi.Provider
	Providers    map[string]*ControlPlane
	pProvider    *multi.Provider
}

func NewFrame(tb testing.TB) *Frame {
	tb.Helper()

	f := new(Frame)
	f.Coordination = startControlPlane(tb)
	f.Compute = startControlPlane(tb)

	f.Consumers = make(map[string]*ControlPlane)
	f.cProvider = multi.New(multi.Options{})

	f.Providers = make(map[string]*ControlPlane)
	f.pProvider = multi.New(multi.Options{})

	return f
}

func (f *Frame) NewConsumer(tb testing.TB, name string, rules ...[]rbacv1.PolicyRule) *ControlPlane {
	tb.Helper()
	require.NotContains(tb, f.Consumers, name, "consumer already exists")
	cp := startControlPlane(tb)
	if len(rules) > 0 {
		cp.AddUser(tb, "broker", rules[0])
	}
	f.Consumers[name] = cp
	require.NoError(tb, f.cProvider.AddProvider(name, cp.Provider()))
	return cp
}

func (f *Frame) NewProvider(tb testing.TB, name string, rules ...[]rbacv1.PolicyRule) *ControlPlane {
	tb.Helper()
	require.NotContains(tb, f.Providers, name, "provider already exists")
	cp := startControlPlane(tb)
	if len(rules) > 0 {
		cp.AddUser(tb, "broker", rules[0])
	}
	f.Providers[name] = cp
	require.NoError(tb, f.pProvider.AddProvider(name, cp.Provider()))
	return cp
}

func (f *Frame) Options(tb testing.TB) manager.Options {
	coordConfig := f.Coordination.Config
	if f.Coordination.brokerCluster != nil {
		coordConfig = f.Coordination.brokerCluster.GetConfig()
	}
	return manager.Options{
		Name:               tb.Name(),
		Local:              f.Coordination.Config, // Admin for leader election
		ComputeConfig:      f.Compute.Config,
		CoordinationConfig: coordConfig,
		Consumer:           f.cProvider,
		Provider:           f.pProvider,
		MgrOptions:         ManagerOptions(),
	}
}

type ControlPlane struct {
	Env           *envtest.Environment
	Config        *rest.Config
	Cluster       cluster.Cluster // Admin cluster for test setup/assertions
	brokerCluster cluster.Cluster // Scoped cluster for broker (nil = use admin)
}

// AddUser creates a scoped user with the given RBAC rules on this control plane.
// The scoped cluster is used by the broker (via Provider()), while the admin
// Cluster remains available for test setup and assertions.
func (c *ControlPlane) AddUser(tb testing.TB, name string, rules []rbacv1.PolicyRule) {
	tb.Helper()

	authUser, err := c.Env.AddUser(envtest.User{Name: name}, nil)
	require.NoError(tb, err)

	scopedConfig := authUser.Config()

	adminClient, err := client.New(c.Config, client.Options{})
	require.NoError(tb, err)

	err = adminClient.Create(tb.Context(), &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Rules:      rules,
	})
	require.NoError(tb, err)

	err = adminClient.Create(tb.Context(), &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     name,
		},
		Subjects: []rbacv1.Subject{
			{Kind: "User", Name: name, APIGroup: "rbac.authorization.k8s.io"},
		},
	})
	require.NoError(tb, err)

	c.brokerCluster, err = cluster.New(scopedConfig)
	require.NoError(tb, err)

	go func() {
		assert.NoError(tb, c.brokerCluster.Start(tb.Context()))
	}()
}

func startControlPlane(tb testing.TB) *ControlPlane {
	tb.Helper()

	c := &ControlPlane{}
	// c.Env, c.Config = utils.DefaultEnvTest(tb)

	var err error
	c.Cluster, err = cluster.New(c.Config)
	require.NoError(tb, err)

	go func() {
		assert.NoError(tb, c.Cluster.Start(tb.Context()))
	}()

	return c
}

func (c *ControlPlane) Provider() multicluster.Provider {
	cl := c.brokerCluster
	if cl == nil {
		cl = c.Cluster
	}
	return single.New("cluster", cl)
}

// brokerCRDRules returns RBAC rules for broker CRDs needed on all clusters due to #132.
func brokerCRDRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{"broker.platform-mesh.io"}, Resources: []string{"acceptapis", "migrationconfigurations", "migrations"}, Verbs: []string{"get", "list", "watch", "update"}},
		{APIGroups: []string{"broker.platform-mesh.io"}, Resources: []string{"acceptapis/finalizers", "migrationconfigurations/finalizers", "migrations/finalizers"}, Verbs: []string{"update"}},
		{APIGroups: []string{"broker.platform-mesh.io"}, Resources: []string{"acceptapis/status", "migrationconfigurations/status"}, Verbs: []string{"get"}},
		{APIGroups: []string{"broker.platform-mesh.io"}, Resources: []string{"migrations/status"}, Verbs: []string{"get", "update", "patch"}},
	}
}

// resourceRules returns RBAC rules for full CRUD + status + finalizers on a resource.
func resourceRules(apiGroup string, resource string) []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{apiGroup}, Resources: []string{resource}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{apiGroup}, Resources: []string{resource + "/status"}, Verbs: []string{"get", "update", "patch"}},
		{APIGroups: []string{apiGroup}, Resources: []string{resource + "/finalizers"}, Verbs: []string{"update"}},
	}
}

// consumerRules builds RBAC rules for a consumer cluster watching the given resources.
func consumerRules(resources ...rbacv1.PolicyRule) []rbacv1.PolicyRule {
	rules := brokerCRDRules()
	return append(rules, resources...)
}

// providerRules builds RBAC rules for a provider cluster watching the given resources.
func providerRules(resources ...rbacv1.PolicyRule) []rbacv1.PolicyRule {
	return consumerRules(resources...)
}

// coordinationRules returns RBAC rules for the coordination cluster,
// including broker CRDs, migration management, and watched resource types.
func coordinationRules(resources ...rbacv1.PolicyRule) []rbacv1.PolicyRule {
	rules := brokerCRDRules()
	// The broker creates and manages Migrations on the coordination cluster.
	rules = append(rules, rbacv1.PolicyRule{
		APIGroups: []string{"broker.platform-mesh.io"},
		Resources: []string{"migrations"},
		Verbs:     []string{"create", "delete", "patch"},
	})
	return append(rules, resources...)
}
