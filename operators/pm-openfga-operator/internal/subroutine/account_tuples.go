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

package subroutine

import (
	"context"
	"fmt"
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/fga"
	platformmeshpath "go.platform-mesh.io/security-operator/internal/platformmesh"
	"go.platform-mesh.io/subroutines"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

// AccountTuplesSubroutine creates FGA tuples for Accounts not of the
// "org"-type when initializing, and deletes them when terminating.
type AccountTuplesSubroutine struct {
	mgr             mcmanager.Manager
	fga             openfgav1.OpenFGAServiceClient
	storeIDGetter   fga.StoreIDGetter
	objectType      string
	parentRelation  string
	creatorRelation string
	kcpClientGetter iclient.KCPClientGetter
}

// Process implements subroutines.Processor.
func (s *AccountTuplesSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	return s.reconcile(ctx, obj)
}

// Initialize implements subroutines.Initializer.
func (s *AccountTuplesSubroutine) Initialize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	return s.reconcile(ctx, obj)
}

func (s *AccountTuplesSubroutine) reconcile(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	lc := obj.(*kcpcorev1alpha1.LogicalCluster)

	accountPath, err := platformmeshpath.NewAccountPathFromLogicalCluster(lc)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting AccountPath from LogicalCluster: %w", err)
	}

	storeID, err := s.storeIDGetter.Get(ctx, accountPath.Org().Base())
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting store ID: %w", err)
	}

	// Determine the parent's and grandParent's LogicalCluster ID
	parentPath, _ := accountPath.Parent()
	parentAccountClusterID, parentAccountLC, err := s.clusterAndIDFromLogicalClusterForPath(ctx, parentPath)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting parent account's LogicalCluster: %w", err)
	}
	grandParentAccountClusterID := parentAccountLC.Spec.Owner.Cluster

	// Retrieve the Account resource out of the parent workspace to determine
	// the creator
	parentAccountClient, err := s.kcpClientGetter.NewClientForLogicalCluster(ctx, parentPath.String())
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting client for parent account cluster: %w", err)
	}
	var acc pmcorev1alpha1.Account
	if err := parentAccountClient.Get(ctx, ctrlruntimeclient.ObjectKey{
		Name: accountPath.Base(),
	}, &acc); err != nil {
		return subroutines.OK(), fmt.Errorf("getting Account in parent account cluster: %w", err)
	}
	if acc.Spec.Creator == nil || *acc.Spec.Creator == "" {
		return subroutines.OK(), fmt.Errorf("account creator is nil or empty")
	}

	tuples, err := fga.InitialTuplesForAccount(fga.InitialTuplesForAccountInput{
		BaseTuplesInput: fga.BaseTuplesInput{
			Creator:                *acc.Spec.Creator,
			AccountOriginClusterID: parentAccountClusterID,
			AccountName:            accountPath.Base(),
			CreatorRelation:        s.creatorRelation,
			ObjectType:             s.objectType,
		},
		ParentOriginClusterID: grandParentAccountClusterID,
		ParentName:            parentPath.Base(),
		ParentRelation:        s.parentRelation,
	})
	if err != nil {
		return subroutines.OK(), fmt.Errorf("building tuples for account: %w", err)
	}
	if err := fga.NewTupleManager(s.fga, storeID, fga.AuthorizationModelIDLatest, logger.LoadLoggerFromContext(ctx)).Apply(ctx, tuples); err != nil {
		return subroutines.OK(), fmt.Errorf("applying tuples for Account: %w", err)
	}

	return subroutines.OK(), nil
}

// Terminate implements subroutines.Terminator.
func (s *AccountTuplesSubroutine) Terminate(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	lc := obj.(*kcpcorev1alpha1.LogicalCluster)

	accountPath, err := platformmeshpath.NewAccountPathFromLogicalCluster(lc)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting AccountPath from LogicalCluster: %w", err)
	}
	parentPath, _ := accountPath.Parent()

	// Determine the parent's LogicalClusterID
	parentClusterID, _, err := s.clusterAndIDFromLogicalClusterForPath(ctx, parentPath)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting parent account's LogicalCluster: %w", err)
	}

	storeID, err := s.storeIDGetter.Get(ctx, accountPath.Org().Base())
	if err != nil {
		if fga.IsStoreNotFound(err) {
			logger.LoadLoggerFromContext(ctx).Info().
				Str("org", accountPath.Org().Base()).
				Msg("org store already deleted, skipping account tuple cleanup")
			return subroutines.OK(), nil
		}
		return subroutines.OK(), fmt.Errorf("getting store ID: %w", err)
	}

	// List tuples that reference the account.
	tm := fga.NewTupleManager(s.fga, storeID, fga.AuthorizationModelIDLatest, logger.LoadLoggerFromContext(ctx))
	accountReferenceTuples, err := tm.ListWithKey(ctx, fga.ReferencingAccountTupleKey(s.objectType, parentClusterID, accountPath.Base()))
	if err != nil {
		if fga.IsStoreNotFound(err) {
			return subroutines.OK(), nil
		}
		return subroutines.OK(), fmt.Errorf("listing tuples referencing Account: %w", err)
	}
	accountTuples := make([]pmcorev1alpha1.Tuple, 0, len(accountReferenceTuples)*2)
	accountTuples = append(accountTuples, accountReferenceTuples...)

	// From tuples referencing the account, parse potential roles specific to the account.
	rolePrefix := fga.RenderRolePrefix(s.objectType, parentClusterID, accountPath.Base())
	for _, t := range accountReferenceTuples {
		if strings.HasPrefix(t.User, rolePrefix) {
			role := strings.TrimSuffix(t.User, "#assignee")
			roleReferences, err := tm.ListWithKey(ctx, &openfgav1.ReadRequestTupleKey{Object: role})
			if err != nil {
				if fga.IsStoreNotFound(err) {
					return subroutines.OK(), nil
				}
				return subroutines.OK(), fmt.Errorf("listing tuples for role %s: %w", role, err)
			}
			accountTuples = append(accountTuples, roleReferences...)
		}
	}

	// Delete all collected tuples.
	if err := tm.Delete(ctx, accountTuples); err != nil {
		if fga.IsStoreNotFound(err) {
			return subroutines.OK(), nil
		}
		return subroutines.OK(), fmt.Errorf("deleting tuples for Account: %w", err)
	}

	return subroutines.OK(), nil
}

// GetName implements subroutines.Subroutine.
func (s *AccountTuplesSubroutine) GetName() string { return "AccountTuplesSubroutine" }

func NewAccountTuplesSubroutine(mgr mcmanager.Manager, fga openfgav1.OpenFGAServiceClient, storeIDGetter fga.StoreIDGetter, creatorRelation, parentRelation, objectType string, kcpHelper iclient.KCPClientGetter) *AccountTuplesSubroutine {
	return &AccountTuplesSubroutine{
		mgr:             mgr,
		fga:             fga,
		storeIDGetter:   storeIDGetter,
		creatorRelation: creatorRelation,
		parentRelation:  parentRelation,
		objectType:      objectType,
		kcpClientGetter: kcpHelper,
	}
}

var (
	_ subroutines.Initializer = &AccountTuplesSubroutine{}
	_ subroutines.Processor   = &AccountTuplesSubroutine{}
	_ subroutines.Terminator  = &AccountTuplesSubroutine{}
)

// clusterAndIDFromLogicalClusterForPath retrieves the LogicalCluster of a given
// path and returns its cluster ID and the LogicalCluster object.
func (s *AccountTuplesSubroutine) clusterAndIDFromLogicalClusterForPath(ctx context.Context, p logicalcluster.Path) (string, kcpcorev1alpha1.LogicalCluster, error) {
	var lc kcpcorev1alpha1.LogicalCluster

	clusterClient, err := s.kcpClientGetter.NewClientForLogicalCluster(ctx, p.String())
	if err != nil {
		return "", lc, fmt.Errorf("getting account cluster client: %w", err)
	}
	if err := clusterClient.Get(ctx, ctrlruntimeclient.ObjectKey{
		Name: "cluster",
	}, &lc); err != nil {
		return "", lc, fmt.Errorf("getting account's LogicalCluster: %w", err)
	}

	clusterID, ok := lc.Annotations["kcp.io/cluster"]
	if !ok || clusterID == "" {
		return "", lc, fmt.Errorf("cluster-annotation kcp.io/cluster on LogicalCluster is not set")
	}

	return clusterID, lc, nil
}
