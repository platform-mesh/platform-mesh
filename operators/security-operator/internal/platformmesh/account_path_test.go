package platformmeshpath

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kcpcore "github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func TestNewAccountPath(t *testing.T) {
	t.Run("returns no error for org path", func(t *testing.T) {
		path, err := NewAccountPath("root:orgs:default")
		require.NoError(t, err)
		assert.Equal(t, "root:orgs:default", path.String())
	})

	t.Run("returns no error for account path", func(t *testing.T) {
		path, err := NewAccountPath("root:orgs:default:testaccount")
		require.NoError(t, err)
		assert.Equal(t, "root:orgs:default:testaccount", path.String())
	})

	t.Run("returns no error for subaccount path", func(t *testing.T) {
		path, err := NewAccountPath("root:orgs:default:testaccount")
		require.NoError(t, err)
		assert.Equal(t, "root:orgs:default:testaccount", path.String())
	})

	t.Run("returns error for invalid path", func(t *testing.T) {
		_, err := NewAccountPath("invalid-path")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a valid platform mesh path")
	})

	t.Run("returns error for path out of orgs workspace", func(t *testing.T) {
		_, err := NewAccountPath("root:platform-mesh-system")
		assert.Error(t, err)
	})

	t.Run("returns error for orgs workspace", func(t *testing.T) {
		_, err := NewAccountPath("root:orgs")
		assert.Error(t, err)
	})
}

func TestAccountPath_IsOrg(t *testing.T) {
	t.Run("returns true for org path", func(t *testing.T) {
		path, err := NewAccountPath("root:orgs:default")
		require.NoError(t, err)
		assert.True(t, path.IsOrg())
	})

	t.Run("returns false for workspace path", func(t *testing.T) {
		path, err := NewAccountPath("root:orgs:default:testaccount")
		require.NoError(t, err)
		assert.False(t, path.IsOrg())
	})

	t.Run("returns false for nested workspace path", func(t *testing.T) {
		path, err := NewAccountPath("root:orgs:default:testaccount:subaccount")
		require.NoError(t, err)
		assert.False(t, path.IsOrg())
	})
}

func TestAccountPath_Org(t *testing.T) {
	t.Run("returns self for org path", func(t *testing.T) {
		path, err := NewAccountPath("root:orgs:default")
		require.NoError(t, err)
		org := path.Org()
		assert.Equal(t, "root:orgs:default", org.String())
	})

	t.Run("returns parent org for workspace path", func(t *testing.T) {
		path, err := NewAccountPath("root:orgs:default:testaccount")
		require.NoError(t, err)
		org := path.Org()
		assert.Equal(t, "root:orgs:default", org.String())
	})

	t.Run("returns root org for deeply nested path", func(t *testing.T) {
		path, err := NewAccountPath("root:orgs:default:testaccount:subaccount")
		require.NoError(t, err)
		org := path.Org()
		assert.Equal(t, "root:orgs:default", org.String())
	})
}

func TestNewAccountPathFromLogicalCluster(t *testing.T) {
	t.Run("returns error when annotation is missing", func(t *testing.T) {
		lc := &kcpcorev1alpha1.LogicalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		}
		_, err := NewAccountPathFromLogicalCluster(lc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), kcpcore.LogicalClusterPathAnnotationKey)
	})

	t.Run("returns error when annotation value is not a valid account path", func(t *testing.T) {
		lc := &kcpcorev1alpha1.LogicalCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "cluster",
				Annotations: map[string]string{kcpcore.LogicalClusterPathAnnotationKey: "root:orgs"},
			},
		}
		_, err := NewAccountPathFromLogicalCluster(lc)
		assert.Error(t, err)
	})

	t.Run("returns AccountPath for valid annotation", func(t *testing.T) {
		lc := &kcpcorev1alpha1.LogicalCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "cluster",
				Annotations: map[string]string{kcpcore.LogicalClusterPathAnnotationKey: "root:orgs:myorg:myaccount"},
			},
		}
		path, err := NewAccountPathFromLogicalCluster(lc)
		require.NoError(t, err)
		assert.Equal(t, "root:orgs:myorg:myaccount", path.String())
	})
}

func TestIsPlatformMeshAccountPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"valid org path", "root:orgs:default", true},
		{"valid workspace path", "root:orgs:default:testaccount", true},
		{"invalid - wrong prefix", "root:other:default", false},
		{"invalid - too few segments", "root:orgs", false},
		{"invalid - single segment", "root", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPlatformMeshAccountPath(tt.path)
			assert.Equal(t, tt.expected, got)
		})
	}
}
