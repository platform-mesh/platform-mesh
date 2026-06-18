package predicates

import (
	"slices"

	"github.com/rs/zerolog/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func HasInitializerPredicate(initializerName string) predicate.Predicate {
	initializer := kcpcorev1alpha1.LogicalClusterInitializer(initializerName)
	return predicate.NewPredicateFuncs(func(object client.Object) bool {
		lc, ok := object.(*kcpcorev1alpha1.LogicalCluster)
		if !ok {
			log.Error().Msg("received non-LogicalCluster resource in HasInitializer predicate")
			return false
		}
		return shouldReconcile(lc, initializer)
	})
}

func shouldReconcile(lc *kcpcorev1alpha1.LogicalCluster, initializer kcpcorev1alpha1.LogicalClusterInitializer) bool {
	return slices.Contains(lc.Spec.Initializers, initializer) && !slices.Contains(lc.Status.Initializers, initializer)
}
