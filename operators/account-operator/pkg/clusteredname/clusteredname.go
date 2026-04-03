package clusteredname

import (
	"context"

	"github.com/kcp-dev/logicalcluster/v3"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

type ClusteredName struct {
	types.NamespacedName
	ClusterID logicalcluster.Name
}

func GetClusteredName(ctx context.Context, obj client.Object) (ClusteredName, bool) {
	clusterName, ok := mccontext.ClusterFrom(ctx)
	cn := ClusteredName{
		NamespacedName: types.NamespacedName{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
		},
	}
	if ok {
		cn.ClusterID = logicalcluster.Name(clusterName)
	}
	return cn, ok
}

func MustGetClusteredName(ctx context.Context, obj client.Object) ClusteredName {
	if cn, ok := GetClusteredName(ctx, obj); ok {
		return cn
	}
	panic("cluster not found in context, cannot requeue")
}
