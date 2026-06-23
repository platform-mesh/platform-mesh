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
	"time"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"go.platform-mesh.io/apis/search/v1alpha1"
	"go.platform-mesh.io/search-operator/internal/metrics"
	"go.platform-mesh.io/search-operator/internal/opensearch"
)

// IndexLifecycleSubroutine manages the lifecycle of OpenSearch indices
type IndexLifecycleSubroutine struct {
	mgr               mcmanager.Manager
	osClient          *opensearch.Client
	staticIndexPrefix string
	semanticModelID   string
}

// NewIndexLifecycleSubroutine creates a new index lifecycle subroutine
func NewIndexLifecycleSubroutine(
	mgr mcmanager.Manager,
	osClient *opensearch.Client,
	staticIndexPrefix string,
	semanticModelID string,
) *IndexLifecycleSubroutine {
	return &IndexLifecycleSubroutine{
		mgr:               mgr,
		osClient:          osClient,
		staticIndexPrefix: normalizePrefix(staticIndexPrefix),
		semanticModelID:   strings.TrimSpace(semanticModelID),
	}
}

var _ lifecyclesubroutine.Subroutine = &IndexLifecycleSubroutine{}

const (
	searchIndexFinalizer = "search.platform-mesh.io/index"
	// Used by clients (e.g. search service) for label selectors.
	searchIndexOrgClusterIDLabel      = "search.platform-mesh.io/org-cluster-id"
	searchIndexOrgClusterIDAnnotation = "search.platform-mesh.io/org-cluster-id"
)

// GetName returns the subroutine name
func (s *IndexLifecycleSubroutine) GetName() string {
	return "IndexLifecycle"
}

// Finalizers returns the finalizers this subroutine manages
func (s *IndexLifecycleSubroutine) Finalizers(instance runtimeobject.RuntimeObject) []string {
	_, ok := instance.(*v1alpha1.SearchIndex)
	if !ok {
		return nil
	}

	return []string{searchIndexFinalizer}
}

// Process handles the reconciliation logic
func (s *IndexLifecycleSubroutine) Process(ctx context.Context, instance runtimeobject.RuntimeObject) (result ctrl.Result, opErr errors.OperatorError) {
	start := time.Now()
	defer func() {
		labelResult := "success"
		if opErr != nil {
			labelResult = "error"
		}
		metrics.SubroutineTotal.WithLabelValues(s.GetName(), labelResult).Inc()
		metrics.SubroutineDuration.WithLabelValues(s.GetName()).Observe(time.Since(start).Seconds())
	}()
	log := logger.LoadLoggerFromContext(ctx)
	searchIndex, ok := instance.(*v1alpha1.SearchIndex)
	if !ok {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("expected *v1alpha1.SearchIndex, got %T", instance), false, false)
	}

	organizationClusterID := searchIndex.Spec.OrganizationClusterID
	if organizationClusterID == "" {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("missing required spec.organizationClusterID"), false, false)
	}
	if err := s.ensureSearchIndexMetadata(ctx, searchIndex, organizationClusterID); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("ensure SearchIndex metadata: %w", err), true, true)
	}

	paused := searchIndex.Spec.Paused

	numberShards := searchIndex.Spec.NumberOfShards
	if numberShards <= 0 {
		numberShards = 1
	}

	numReplicas := max(searchIndex.Spec.NumberOfReplicas, 0)
	desiredIndexName := searchIndex.Name

	log.Info().
		Str("name", searchIndex.GetName()).
		Str("organizationClusterID", organizationClusterID).
		Str("desiredIndexName", desiredIndexName).
		Bool("paused", paused).
		Int32("numberOfShards", numberShards).
		Int32("numberOfReplicas", numReplicas).
		Msg("processing SearchIndex")

	if paused {
		return ctrl.Result{}, nil
	}

	if s.osClient == nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("OpenSearch client not configured"), true, false)
	}

	legacyIndexName := organizationClusterID
	useIndexName := desiredIndexName

	desiredExists, err := s.osClient.IndexExists(ctx, desiredIndexName)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to check index existence for %q: %w", desiredIndexName, err), true, true)
	}
	legacyExists := false
	if !desiredExists {
		legacyExists, err = s.osClient.IndexExists(ctx, legacyIndexName)
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to check legacy index existence for %q: %w", legacyIndexName, err), true, true)
		}
		if legacyExists {
			useIndexName = legacyIndexName
		}
	}

	created := false
	replicasUpdated := false
	if !desiredExists && !legacyExists {
		mapping, err := opensearch.DefaultIndexMapping(searchIndex.Spec.SemanticFields, s.semanticModelID)
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to build index mapping for %q: %w", desiredIndexName, err), false, false)
		}
		if err := s.osClient.CreateIndex(ctx, desiredIndexName, numberShards, numReplicas, mapping); err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to create index %q: %w", desiredIndexName, err), true, true)
		}
		created = true
		useIndexName = desiredIndexName
	} else {
		currentSettings, settingsErr := s.osClient.GetIndexSettings(ctx, useIndexName)
		if settingsErr != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to read index settings for %q: %w", useIndexName, settingsErr), true, true)
		}

		if currentSettings.NumberOfShards != numberShards {
			return ctrl.Result{}, errors.NewOperatorError(
				fmt.Errorf(
					"cannot change number_of_shards for existing index %q (current=%d desired=%d); create a new index and reindex data",
					useIndexName,
					currentSettings.NumberOfShards,
					numberShards,
				),
				false,
				false,
			)
		}

		if currentSettings.NumberOfReplicas != numReplicas {
			if err := s.osClient.UpdateIndexReplicas(ctx, useIndexName, numReplicas); err != nil {
				return ctrl.Result{}, errors.NewOperatorError(
					fmt.Errorf("failed to update number_of_replicas for index %q to %d: %w", useIndexName, numReplicas, err),
					true,
					true,
				)
			}

			log.Info().
				Str("name", searchIndex.GetName()).
				Str("indexName", useIndexName).
				Int32("previousNumberOfReplicas", currentSettings.NumberOfReplicas).
				Int32("numberOfReplicas", numReplicas).
				Msg("updated existing index replicas")
			replicasUpdated = true
		}
	}

	aliases := buildIndexAliases(s.staticIndexPrefix, organizationClusterID, desiredIndexName)
	if err := s.osClient.EnsureAliases(ctx, useIndexName, aliases); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to ensure aliases for index %q: %w", useIndexName, err), true, true)
	}

	statusChanged := false
	if searchIndex.Status.IndexName != useIndexName {
		searchIndex.Status.IndexName = useIndexName
		statusChanged = true
	}

	if created || replicasUpdated || statusChanged {
		searchIndex.Status.LastSyncTime = &v1.Time{Time: time.Now()}
		log.Info().
			Str("name", searchIndex.GetName()).
			Str("organizationClusterID", organizationClusterID).
			Str("indexName", useIndexName).
			Str("desiredIndexName", desiredIndexName).
			Bool("created", created).
			Bool("legacyIndexInUse", useIndexName == legacyIndexName && useIndexName != desiredIndexName).
			Bool("replicasUpdated", replicasUpdated).
			Bool("statusChanged", statusChanged).
			Int32("numberOfShards", numberShards).
			Int32("numberOfReplicas", numReplicas).
			Msg("updated SearchIndex status")
	}

	return ctrl.Result{}, nil
}

// Finalize handles cleanup when the resource is being deleted
func (s *IndexLifecycleSubroutine) Finalize(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	log := logger.LoadLoggerFromContext(ctx)
	searchIndex, ok := instance.(*v1alpha1.SearchIndex)
	if !ok {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("expected *v1alpha1.SearchIndex, got %T", instance), false, false)
	}

	if s.osClient == nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("OpenSearch client not configured during SearchIndex finalization"), true, false)
	}

	workspaceName, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("missing cluster in multicluster context during finalization"), true, false)
	}

	indexName := searchIndex.Status.IndexName
	if indexName == "" {
		log.Warn().
			Str("name", searchIndex.GetName()).
			Str("workspace", workspaceName.String()).
			Msg("SearchIndex has no indexName in status; skipping OpenSearch cleanup")
		return ctrl.Result{}, nil
	}

	log.Info().
		Str("name", searchIndex.GetName()).
		Str("reconcileWorkspace", workspaceName.String()).
		Str("indexName", indexName).
		Msg("finalizing SearchIndex")

	if err := s.osClient.DeleteIndex(ctx, indexName); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to delete index %q: %w", indexName, err), true, true)
	}

	return ctrl.Result{}, nil
}

func (s *IndexLifecycleSubroutine) ensureSearchIndexMetadata(ctx context.Context, si *v1alpha1.SearchIndex, orgClusterID string) error {
	original := si.DeepCopy()
	changed := false

	if si.Labels == nil {
		si.Labels = map[string]string{}
	}
	if current := strings.TrimSpace(si.Labels[searchIndexOrgClusterIDLabel]); current != orgClusterID {
		si.Labels[searchIndexOrgClusterIDLabel] = orgClusterID
		changed = true
	}

	if si.Annotations == nil {
		si.Annotations = map[string]string{}
	}
	if current := strings.TrimSpace(si.Annotations[searchIndexOrgClusterIDAnnotation]); current != orgClusterID {
		si.Annotations[searchIndexOrgClusterIDAnnotation] = orgClusterID
		changed = true
	}

	if !changed {
		return nil
	}

	cluster, err := s.mgr.ClusterFromContext(ctx)
	if err != nil {
		return fmt.Errorf("get cluster from context: %w", err)
	}

	if err := cluster.GetClient().Patch(ctx, si, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("patch SearchIndex metadata: %w", err)
	}

	return nil
}

func buildIndexAliases(staticPrefix, organizationClusterID, canonicalIndexName string) []string {
	static := sanitizeIndexNamePart(staticPrefix)
	orgID := sanitizeIndexNamePart(organizationClusterID)
	canonical := sanitizeIndexNamePart(canonicalIndexName)

	aliases := make([]string, 0, 3)
	if static != "" {
		aliases = append(aliases, fmt.Sprintf("%s-all", static))
	}
	if canonical != "" && canonical != orgID {
		aliases = append(aliases, canonical)
	}

	return aliases
}

func normalizePrefix(value string) string {
	if sanitized := sanitizeIndexNamePart(value); sanitized != "" {
		return sanitized
	}
	return "pm"
}
