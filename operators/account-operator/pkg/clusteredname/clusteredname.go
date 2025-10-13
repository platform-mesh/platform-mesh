package clusteredname

import (
	"context"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"k8s.io/apimachinery/pkg/types"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

type ClusteredName struct {
	types.NamespacedName
	ClusterID logicalcluster.Name
}

func GetClusteredName(ctx context.Context, instance runtimeobject.RuntimeObject) (ClusteredName, bool) {
	clusterName, ok := mccontext.ClusterFrom(ctx)
	cn := ClusteredName{
		NamespacedName: types.NamespacedName{
			Name:      instance.GetName(),
			Namespace: instance.GetNamespace(),
		},
	}
	if ok {
		cn.ClusterID = logicalcluster.Name(clusterName)
	}
	return cn, ok
}

func MustGetClusteredName(ctx context.Context, instance runtimeobject.RuntimeObject) ClusteredName {
	if cn, ok := GetClusteredName(ctx, instance); ok {
		return cn
	}
	panic("cluster not found in context, cannot requeue")
}
