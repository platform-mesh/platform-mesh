//go:build integration

package projector_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"go.platform-mesh.io/backup-operator/pkg/topology/projector"
)

func setupEnvtest(t *testing.T) (client.Client, *rest.Config, func()) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	assetsDir := os.Getenv("KUBEBUILDER_ASSETS")
	if assetsDir == "" {
		assetsDir = filepath.Join("..", "..", "..", "bin", "k8s")
	}

	env := &envtest.Environment{
		BinaryAssetsDirectory: assetsDir,
	}
	cfg, err := env.Start()
	require.NoError(t, err, "starting envtest")

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	return c, cfg, func() {
		require.NoError(t, env.Stop())
	}
}

const testNamespace = "default"

// i. EnsureConfigMap creates the ConfigMap when absent.
func TestEnsureConfigMap_Creates(t *testing.T) {
	c, _, stop := setupEnvtest(t)
	defer stop()

	p := projector.New(c, testNamespace)
	require.NoError(t, p.EnsureConfigMap(context.Background()))

	var cm corev1.ConfigMap
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{
		Name:      "backup-topology-schemas",
		Namespace: testNamespace,
	}, &cm))

	assert.Contains(t, cm.Data, "v1alpha1.json")
	assert.NotEmpty(t, cm.Data["v1alpha1.json"])
}

// j. EnsureConfigMap is idempotent when called twice.
func TestEnsureConfigMap_Idempotent(t *testing.T) {
	c, _, stop := setupEnvtest(t)
	defer stop()

	p := projector.New(c, testNamespace)
	require.NoError(t, p.EnsureConfigMap(context.Background()))
	require.NoError(t, p.EnsureConfigMap(context.Background()), "second call must not error")

	var cm corev1.ConfigMap
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{
		Name:      "backup-topology-schemas",
		Namespace: testNamespace,
	}, &cm))

	assert.Contains(t, cm.Data, "v1alpha1.json")
}

// k. EnsureConfigMap updates data when called after an external patch.
func TestEnsureConfigMap_Updates(t *testing.T) {
	c, _, stop := setupEnvtest(t)
	defer stop()

	p := projector.New(c, testNamespace)
	require.NoError(t, p.EnsureConfigMap(context.Background()))

	// Simulate external drift: patch the ConfigMap data to stale content.
	var cm corev1.ConfigMap
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{
		Name:      "backup-topology-schemas",
		Namespace: testNamespace,
	}, &cm))
	patch := client.MergeFrom(cm.DeepCopy())
	cm.Data["v1alpha1.json"] = `{"stale": true}`
	require.NoError(t, c.Patch(context.Background(), &cm, patch))

	// Re-apply — should restore the correct schema.
	require.NoError(t, p.EnsureConfigMap(context.Background()))

	var updated corev1.ConfigMap
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{
		Name:      "backup-topology-schemas",
		Namespace: testNamespace,
	}, &updated))

	assert.NotEqual(t, `{"stale": true}`, updated.Data["v1alpha1.json"])
	assert.Contains(t, updated.Data["v1alpha1.json"], "schemaVersion")
}
