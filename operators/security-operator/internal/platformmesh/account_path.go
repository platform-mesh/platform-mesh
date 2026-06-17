package platformmeshpath

import (
	"fmt"
	"strings"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpcore "github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

const (
	rootWorkspace = "root"
	orgsWorkspace = "orgs"

	kcpWorkpaceSeparator = ":"
)

// IsPlatformMeshAccountPath returns whether a value is a platform-mesh account
// path, i.e. a canonical KCP workspace path child to the platform-mesh account
// workspace tree "root:orgs".
func IsPlatformMeshAccountPath(value string) bool {
	_, valid := logicalcluster.NewValidatedPath(value)
	parts := strings.Split(value, kcpWorkpaceSeparator)

	return valid && len(parts) > 2 && parts[0] == rootWorkspace && parts[1] == orgsWorkspace
}

// AccountPath represents a logicalcluster.Path that is assumed to be the path
// of a platform-mesh Account, i.e. conforms to the conditions of the
// IsPlatformMeshAccountPath function.
type AccountPath struct {
	logicalcluster.Path
}

func NewAccountPath(value string) (AccountPath, error) {
	if !IsPlatformMeshAccountPath(value) {
		return AccountPath{}, fmt.Errorf("%s is not a valid platform mesh path", value)
	}

	return AccountPath{
		Path: logicalcluster.NewPath(value),
	}, nil
}

func NewAccountPathFromLogicalCluster(lc *kcpcorev1alpha1.LogicalCluster) (AccountPath, error) {
	p, ok := lc.Annotations[kcpcore.LogicalClusterPathAnnotationKey]
	if !ok {
		return AccountPath{}, fmt.Errorf("LogicalCluster does not contain %s annotation", kcpcore.LogicalClusterPathAnnotationKey)
	}

	return NewAccountPath(p)
}

// IsOrg returns true if the AccountPath is an organisation.
func (a AccountPath) IsOrg() bool {
	parts := strings.Split(a.String(), kcpWorkpaceSeparator)
	return len(parts) == 3
}

// Org returns the AccountPath's parent organisation.
func (a AccountPath) Org() AccountPath {
	parts := strings.Split(a.String(), kcpWorkpaceSeparator)
	return AccountPath{
		Path: logicalcluster.NewPath(strings.Join(parts[:3], kcpWorkpaceSeparator)),
	}
}
