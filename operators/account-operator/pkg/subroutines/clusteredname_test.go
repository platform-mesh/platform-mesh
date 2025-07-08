package subroutines

import (
	"context"
	"testing"

	"github.com/kcp-dev/logicalcluster/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/kontext"
)

// dummyRuntimeObject implements runtimeobject.RuntimeObject for testing
type dummyRuntimeObject struct {
	metav1.ObjectMeta
}

func (d *dummyRuntimeObject) GetObjectKind() schema.ObjectKind { return nil }
func (d *dummyRuntimeObject) DeepCopyObject() runtime.Object   { return d }
func (d *dummyRuntimeObject) GetName() string                  { return d.Name }
func (d *dummyRuntimeObject) GetNamespace() string             { return d.Namespace }

func TestGetClusteredName_WithCluster(t *testing.T) {
	cluster := logicalcluster.Name("test-cluster")
	ctx := kontext.WithCluster(context.Background(), cluster)
	obj := &dummyRuntimeObject{}
	obj.Name = "foo"
	obj.Namespace = "bar"

	cn, ok := GetClusteredName(ctx, obj)
	if !ok {
		t.Fatalf("expected ok=true, got false")
	}
	if cn.Name != "foo" || cn.Namespace != "bar" || cn.ClusterID != cluster {
		t.Errorf("unexpected ClusteredName: %+v", cn)
	}
}

func TestGetClusteredName_WithoutCluster(t *testing.T) {
	ctx := context.Background()
	obj := &dummyRuntimeObject{}
	obj.Name = "foo"
	obj.Namespace = "bar"

	_, ok := GetClusteredName(ctx, obj)
	if ok {
		t.Fatalf("expected ok=false, got true")
	}
}

func TestMustGetClusteredName_WithCluster(t *testing.T) {
	cluster := logicalcluster.Name("test-cluster")
	ctx := kontext.WithCluster(context.Background(), cluster)
	obj := &dummyRuntimeObject{}
	obj.Name = "foo"
	obj.Namespace = "bar"

	cn := MustGetClusteredName(ctx, obj)
	if cn.Name != "foo" || cn.Namespace != "bar" || cn.ClusterID != cluster {
		t.Errorf("unexpected ClusteredName: %+v", cn)
	}
}

func TestMustGetClusteredName_WithoutCluster(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic, got none")
		}
	}()
	ctx := context.Background()
	obj := &dummyRuntimeObject{}
	obj.Name = "foo"
	obj.Namespace = "bar"

	_ = MustGetClusteredName(ctx, obj)
}
