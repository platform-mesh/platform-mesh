package predicates

import (
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

const kcpPathAnnotation = "kcp.io/path"

// LogicalClusterIsAccountTypeOrg returns a predicate that filters for
// LogicalClusters belonging to an Account of type "org", i.e. is a child of the
// "root:orgs" cluster.
func LogicalClusterIsAccountTypeOrg() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(object client.Object) bool {
		lc, ok := object.(*kcpcorev1alpha1.LogicalCluster)
		if !ok {
			panic(fmt.Errorf("received non-LogicalCluster resource in LogicalClusterIsAccountTypeOrg predicate"))
		}
		p := lc.Annotations[kcpPathAnnotation]

		parts := strings.Split(p, ":")

		return len(parts) == 3 && parts[0] == "root" && parts[1] == "orgs"
	})
}
