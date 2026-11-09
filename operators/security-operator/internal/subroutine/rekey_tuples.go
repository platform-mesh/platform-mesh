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
	"maps"
	"slices"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/fga"
	"go.platform-mesh.io/security-operator/internal/metrics"
	"go.platform-mesh.io/security-operator/pkg/config"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

const (
	rekeyEventComponent = "security-operator"
	// rekeyEventAction is the "action" field of the events API: what this
	// controller did to the org, regardless of the per-event outcome.
	rekeyEventAction = "RekeyOrphanedTuples"
)

// RekeyTuplesSubroutine re-keys FGA tuples that were orphaned by an org
// workspace re-creation: the org's logical cluster hash is embedded in every
// role/account tuple key, so when the workspace is re-created under a new hash
// all pre-existing tuples (memberships, role assignments) silently stop
// matching. The subroutine scans the org's store for tuples that reference the
// org account under a cluster id other than the current one, verifies that the
// old cluster id is actually dead, writes re-keyed copies, and only then
// deletes the originals.
//
// Only keys of the configured account object type (Config.FGA.ObjectType) are
// considered — other key families in the same store share the raw key shape
// (e.g. apis_kcp_io_apiexport user keys) and must never be rewritten. The
// whole pass is skipped when a descendant Account shares the org's name,
// because such an account's tuples are keyed by its (equally dead) parent
// cluster id and cannot be told apart from the org's own.
//
// It is fully gated behind Config.RekeyOrphanedTuplesEnabled and does nothing
// when the flag is off.
type RekeyTuplesSubroutine struct {
	mgr             mcmanager.Manager
	fga             openfgav1.OpenFGAServiceClient
	storeIDGetter   fga.StoreIDGetter
	kcpClientGetter iclient.KCPClientGetter
	cfg             config.Config
}

func NewRekeyTuplesSubroutine(cfg config.Config, mgr mcmanager.Manager, fgaClient openfgav1.OpenFGAServiceClient, storeIDGetter fga.StoreIDGetter, kcpClientGetter iclient.KCPClientGetter) *RekeyTuplesSubroutine {
	return &RekeyTuplesSubroutine{
		mgr:             mgr,
		fga:             fgaClient,
		storeIDGetter:   storeIDGetter,
		kcpClientGetter: kcpClientGetter,
		cfg:             cfg,
	}
}

// GetName implements subroutines.Subroutine.
func (s *RekeyTuplesSubroutine) GetName() string { return "RekeyTuplesSubroutine" }

// Initialize implements subroutines.Initializer.
func (s *RekeyTuplesSubroutine) Initialize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	return s.reconcile(ctx, obj)
}

// Process implements subroutines.Processor.
func (s *RekeyTuplesSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	return s.reconcile(ctx, obj)
}

func (s *RekeyTuplesSubroutine) reconcile(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	if !s.cfg.RekeyOrphanedTuplesEnabled {
		return subroutines.OK(), nil
	}

	log := logger.LoadLoggerFromContext(ctx)
	lc := obj.(*kcpcorev1alpha1.LogicalCluster)

	cluster, err := s.mgr.ClusterFromContext(ctx)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get cluster from context: %w", err)
	}

	var ai pmcorev1alpha1.AccountInfo
	if err := cluster.GetClient().Get(ctx, ctrlruntimeclient.ObjectKey{
		Name: "account",
	}, &ai); err != nil && !apierrors.IsNotFound(err) {
		return subroutines.OK(), fmt.Errorf("getting AccountInfo for LogicalCluster: %w", err)
	} else if apierrors.IsNotFound(err) {
		return subroutines.StopWithRequeue(5*time.Second, "AccountInfo not found yet, requeuing"), nil
	}

	accountName := ai.Spec.Account.Name
	currentClusterID := ai.Spec.Account.OriginClusterId
	if accountName == "" || currentClusterID == "" {
		return subroutines.StopWithRequeue(5*time.Second, "AccountInfo does not carry the account name and origin cluster id yet, requeuing"), nil
	}

	storeID, err := s.storeIDGetter.Get(ctx, accountName)
	if err != nil {
		if fga.IsStoreNotFound(err) {
			return subroutines.StopWithRequeue(5*time.Second, "org store not found yet, requeuing"), nil
		}
		return subroutines.OK(), fmt.Errorf("getting store ID: %w", err)
	}

	objectType := s.cfg.FGA.ObjectType

	tm := fga.NewTupleManager(s.fga, storeID, fga.AuthorizationModelIDLatest, log)
	stale, err := tm.ListWithFilter(ctx, fga.StaleAccountTupleFilter(objectType, accountName, currentClusterID))
	if err != nil {
		return subroutines.OK(), fmt.Errorf("listing stale tuples: %w", err)
	}
	if len(stale) == 0 {
		return subroutines.OK(), nil
	}

	// Positive attribution guard: sub-account tuples are keyed by their PARENT
	// workspace's cluster id (see account_tuples.go), so a descendant Account
	// carrying the org's own name produces tuples that are indistinguishable
	// from the org's after a tree re-creation — both reference accountName
	// under a now-dead cluster id. Re-keying those onto the org's current
	// cluster id would merge the sub-account's role assignees into the org's
	// roles. If such an Account exists in the org workspace, never guess: skip
	// the whole pass.
	var sameNameAccount pmcorev1alpha1.Account
	err = cluster.GetClient().Get(ctx, ctrlruntimeclient.ObjectKey{Name: accountName}, &sameNameAccount)
	switch {
	case err == nil:
		log.Warn().
			Str("account", accountName).
			Int("count", len(stale)).
			Msg("a descendant Account shares the org's name, stale tuples cannot be attributed to the org, skipping re-key")
		metrics.RekeyedTuples.WithLabelValues("skipped_ambiguous").Add(float64(len(stale)))
		cluster.GetEventRecorder(rekeyEventComponent).Eventf(lc, nil, corev1.EventTypeWarning, "OrphanedTupleRekeySkipped", rekeyEventAction,
			"a descendant Account named %q exists in the org workspace; %d stale tuples cannot be attributed to the org, skipping re-key", accountName, len(stale))
		return subroutines.OK(), nil
	case apierrors.IsNotFound(err):
		// No same-name descendant: the stale tuples can be attributed safely.
	default:
		// Fail closed: without the guard's answer no tuple may be touched.
		return subroutines.OK(), fmt.Errorf("checking for a descendant Account named like the org: %w", err)
	}

	// Group the stale tuples by the old cluster id they reference. A tuple
	// referencing more than one stale cluster id is ambiguous — skip it, never
	// guess.
	groups := map[string][]pmcorev1alpha1.Tuple{}
	for _, t := range stale {
		ids := fga.StaleClusterIDs(t, objectType, accountName, currentClusterID)
		if len(ids) != 1 {
			log.Warn().
				Str("object", t.Object).
				Str("user", t.User).
				Msg("stale tuple references multiple old cluster ids, skipping")
			metrics.RekeyedTuples.WithLabelValues("skipped_ambiguous").Inc()
			cluster.GetEventRecorder(rekeyEventComponent).Eventf(lc, nil, corev1.EventTypeWarning, "OrphanedTupleRekeySkipped", rekeyEventAction,
				"tuple (object=%s user=%s) references multiple old cluster ids, skipping re-key", t.Object, t.User)
			continue
		}
		groups[ids[0]] = append(groups[ids[0]], t)
	}

	requeue := false
	for _, oldClusterID := range slices.Sorted(maps.Keys(groups)) {
		group := groups[oldClusterID]

		alive, aliveErr := s.logicalClusterExists(ctx, oldClusterID)
		if aliveErr != nil {
			// Fail closed: only re-key when the liveness check proves the old
			// cluster id dead.
			log.Warn().Err(aliveErr).
				Str("old_cluster_id", oldClusterID).
				Int("count", len(group)).
				Msg("could not verify logical cluster liveness, skipping re-key for this cluster id")
			metrics.RekeyedTuples.WithLabelValues("skipped_error").Add(float64(len(group)))
			requeue = true
			continue
		}
		if alive {
			// The referenced logical cluster still exists — these tuples are
			// not orphaned by this org's re-creation. Never guess.
			log.Info().
				Str("old_cluster_id", oldClusterID).
				Int("count", len(group)).
				Msg("referenced logical cluster is still alive, skipping re-key for this cluster id")
			metrics.RekeyedTuples.WithLabelValues("skipped_live").Add(float64(len(group)))
			cluster.GetEventRecorder(rekeyEventComponent).Eventf(lc, nil, corev1.EventTypeWarning, "OrphanedTupleRekeySkipped", rekeyEventAction,
				"logical cluster %s referenced by %d tuples of account %q is still alive, skipping re-key", oldClusterID, len(group), accountName)
			continue
		}

		rekeyed := make([]pmcorev1alpha1.Tuple, 0, len(group))
		for _, t := range group {
			rekeyed = append(rekeyed, fga.RekeyTuple(t, objectType, oldClusterID, currentClusterID))
		}

		// Write the re-keyed copies first; delete the originals only after all
		// writes succeeded, so an interrupted run never loses tuples.
		if err := tm.ApplyChunked(ctx, rekeyed); err != nil {
			return subroutines.OK(), fmt.Errorf("applying re-keyed tuples for old cluster id %s: %w", oldClusterID, err)
		}
		if err := tm.DeleteChunked(ctx, group); err != nil {
			return subroutines.OK(), fmt.Errorf("deleting stale tuples for old cluster id %s: %w", oldClusterID, err)
		}

		log.Info().
			Str("old_cluster_id", oldClusterID).
			Str("new_cluster_id", currentClusterID).
			Int("count", len(group)).
			Msg("re-keyed orphaned tuples")
		metrics.RekeyedTuples.WithLabelValues("rekeyed").Add(float64(len(group)))
		cluster.GetEventRecorder(rekeyEventComponent).Eventf(lc, nil, corev1.EventTypeNormal, "OrphanedTuplesRekeyed", rekeyEventAction,
			"re-keyed %d FGA tuples of account %q from dead cluster id %s to %s", len(group), accountName, oldClusterID, currentClusterID)
	}

	if requeue {
		return subroutines.StopWithRequeue(30*time.Second, "could not verify liveness for at least one old cluster id, requeuing"), nil
	}
	return subroutines.OK(), nil
}

// logicalClusterExists reports whether the logical cluster behind clusterID
// still exists, by getting its LogicalCluster "cluster" object. kcp answers
// with NotFound or Forbidden for logical clusters that are gone; any other
// error is returned so callers can fail closed.
func (s *RekeyTuplesSubroutine) logicalClusterExists(ctx context.Context, clusterID string) (bool, error) {
	clusterClient, err := s.kcpClientGetter.NewClientForLogicalCluster(ctx, clusterID)
	if err != nil {
		return false, fmt.Errorf("getting client for logical cluster %s: %w", clusterID, err)
	}

	var lc kcpcorev1alpha1.LogicalCluster
	err = clusterClient.Get(ctx, ctrlruntimeclient.ObjectKey{
		Name: "cluster",
	}, &lc)
	switch {
	case err == nil:
		return true, nil
	case apierrors.IsNotFound(err) || apierrors.IsForbidden(err):
		return false, nil
	default:
		return false, fmt.Errorf("getting LogicalCluster for cluster %s: %w", clusterID, err)
	}
}

var (
	_ subroutines.Initializer = &RekeyTuplesSubroutine{}
	_ subroutines.Processor   = &RekeyTuplesSubroutine{}
)
