package subroutines

import (
	"context"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/kontext"
)

type ClusteredName struct {
	types.NamespacedName
	ClusterID logicalcluster.Name
}

func GetClusteredName(ctx context.Context, instance runtimeobject.RuntimeObject) (ClusteredName, bool) {
	var cn ClusteredName
	if cluster, ok := kontext.ClusterFrom(ctx); ok {
		cn = ClusteredName{NamespacedName: types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, ClusterID: cluster}
		return cn, true
	} else {
		return cn, false
	}
}

func MustGetClusteredName(ctx context.Context, instance runtimeobject.RuntimeObject) ClusteredName {
	var cn ClusteredName
	if cluster, ok := kontext.ClusterFrom(ctx); ok {
		cn = ClusteredName{NamespacedName: types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, ClusterID: cluster}
		return cn
	} else {
		panic("cluster not found in context, cannot requeue")
	}
}
