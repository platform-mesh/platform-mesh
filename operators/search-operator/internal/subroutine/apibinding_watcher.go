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
	"sort"
	"time"

	pmsearchv1alpha1 "go.platform-mesh.io/apis/search/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/search-operator/internal/metrics"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
)

// apiBindingWatcherSubroutine watches APIBinding resources across workspaces.
// When a binding takes place in an org then all indexes are updated for the
// fields contained in the bound APIResourceSchemas.
type apiBindingWatcherSubroutine struct {
	mgr                mcmanager.Manager
	orgsClient         ctrlruntimeclient.Client
	searchConfigClient ctrlruntimeclient.Client
	rootCfg            *rest.Config
	indexPrefix        string
}

// NewAPIBindingWatcherSubroutine creates a new APIBinding watcher subroutine.
// orgsClient must be scoped to the root:orgs workspace.
// searchConfigClient must be scoped to the provider workspace.
// localCfg must be the admin kcp REST config.
func NewAPIBindingWatcherSubroutine(mgr mcmanager.Manager, orgsClient ctrlruntimeclient.Client, searchConfigClient ctrlruntimeclient.Client, localCfg *rest.Config, indexPrefix string) (lifecyclesubroutine.Subroutine, error) {
	rootCfg, err := stripPathFromConfig(localCfg)
	if err != nil {
		return nil, err
	}

	return &apiBindingWatcherSubroutine{
		mgr:                mgr,
		orgsClient:         orgsClient,
		searchConfigClient: searchConfigClient,
		rootCfg:            rootCfg,
		indexPrefix:        indexPrefix,
	}, nil
}

var _ lifecyclesubroutine.Subroutine = &apiBindingWatcherSubroutine{}

func (s *apiBindingWatcherSubroutine) GetName() string {
	return "APIBindingWatcher"
}

func (s *apiBindingWatcherSubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string {
	return nil
}

// Process ensures that a SearchIndex exists in the org workspace for each bound
// APIBinding, with fields populated from the bound APIResourceSchemas and any
// SearchConfig found in the operator provider workspace.
func (s *apiBindingWatcherSubroutine) Process(ctx context.Context, instance runtimeobject.RuntimeObject) (result ctrl.Result, opErr errors.OperatorError) {
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
	binding := instance.(*kcpapisv1alpha1.APIBinding)

	if binding.Status.Phase != kcpapisv1alpha1.APIBindingPhaseBound {
		log.Debug().
			Str("name", binding.Name).
			Str("phase", string(binding.Status.Phase)).
			Msg("APIBinding not yet bound, skipping")
		return ctrl.Result{}, nil
	}

	_, workspacePath, err := getWorkspaceClusterAndPath(ctx, s.mgr)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("get workspace path: %w", err), true, false)
	}

	orgName, err := extractOrgFromPath(workspacePath)
	if err != nil {
		log.Debug().Str("workspacePath", workspacePath).Msg("APIBinding is not in an org workspace, skipping")
		return ctrl.Result{}, nil
	}

	orgClusterID, err := getOrgClusterID(ctx, s.orgsClient, orgName)
	if err != nil {
		log.Debug().Err(err).Str("orgName", orgName).Msg("org Workspace not found, requeuing")
		return ctrl.Result{Requeue: true}, nil
	}

	fields, err := s.resolveFieldsForBinding(ctx, binding)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("resolve fields for binding %q: %w", binding.Name, err), true, false)
	}

	for _, br := range binding.Status.AppliedPermissionClaims {
		if err := s.ensureSearchIndex(ctx, log, orgName, orgClusterID, br.Resource, fields); err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("ensure SearchIndex for binding %q resource %q: %w", binding.Name, br.Resource, err), true, false)
		}
	}

	return ctrl.Result{}, nil
}

// Finalize is a no-op: we do not remove the SearchIndex when a binding is deleted
// because the index may still hold indexed data that should persist.
// TODO: there should still be some strategy for cleanup of old SearchIndexes
func (s *apiBindingWatcherSubroutine) Finalize(_ context.Context, _ runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	return ctrl.Result{}, nil
}

// searchIndexFields holds the resolved field lists for a SearchIndex.
type searchIndexFields struct {
	defaultFields    []string
	semanticFields   []string
	filterableFields []string
}

// resolveFieldsForBinding collects the top-level field names from every APIResourceSchema
// referenced by the binding, then applies any SearchConfig found in the operator provider workspace
// to classify fields into default, semantic, and filterable lists.
func (s *apiBindingWatcherSubroutine) resolveFieldsForBinding(ctx context.Context, binding *kcpapisv1alpha1.APIBinding) (*searchIndexFields, error) {
	if len(binding.Status.BoundResources) == 0 {
		return &searchIndexFields{}, nil
	}

	// The export cluster is the provider workspace that owns the APIExport.
	// It is not a consumer of the export, so it does not appear in the multicluster
	// manager's cluster list. Build a direct client using the cluster ID via the
	// clusters API instead of going through GetCluster.
	exportClient, err := buildClusterIDScopedClient(s.rootCfg, s.mgr.GetLocalManager().GetScheme(), binding.Status.APIExportClusterName)
	if err != nil {
		return nil, fmt.Errorf("get export cluster client %q: %w", binding.Status.APIExportClusterName, err)
	}

	seen := make(map[string]struct{})
	for _, br := range binding.Status.BoundResources {
		schema := &kcpapisv1alpha1.APIResourceSchema{}
		if err := exportClient.Get(ctx, types.NamespacedName{Name: br.Schema.Name}, schema); err != nil {
			return nil, fmt.Errorf("get APIResourceSchema %q: %w", br.Schema.Name, err)
		}

		for _, version := range schema.Spec.Versions {
			if !version.Served {
				continue
			}
			props, err := version.GetSchema()
			if err != nil {
				return nil, fmt.Errorf("parse schema for %q version %q: %w", br.Schema.Name, version.Name, err)
			}
			if props == nil {
				continue
			}
			for fieldName := range props.Properties {
				seen[fieldName] = struct{}{}
			}
		}
	}

	allFields := make([]string, 0, len(seen))
	for f := range seen {
		allFields = append(allFields, f)
	}
	sort.Strings(allFields)

	// Try to fetch a SearchConfig from the operator provider workspace to classify fields.
	searchConfig := s.fetchSearchConfig(ctx, binding)
	if searchConfig == nil {
		// No SearchConfig found — fall back to all fields as defaultFields (heuristic).
		return &searchIndexFields{defaultFields: allFields}, nil
	}

	return applySearchConfig(allFields, searchConfig), nil
}

// fetchSearchConfig attempts to load a SearchConfig from the operator provider workspace.
// It looks for a SearchConfig whose name matches any bound resource schema name.
// Returns nil if no SearchConfig is found.
func (s *apiBindingWatcherSubroutine) fetchSearchConfig(ctx context.Context, binding *kcpapisv1alpha1.APIBinding) *pmsearchv1alpha1.SearchConfig {
	log := logger.LoadLoggerFromContext(ctx)

	for _, br := range binding.Status.BoundResources {
		cfg := &pmsearchv1alpha1.SearchConfig{}
		err := s.searchConfigClient.Get(ctx, types.NamespacedName{Name: br.Schema.Name}, cfg)
		if err == nil {
			log.Debug().
				Str("searchConfig", cfg.Name).
				Str("schema", br.Schema.Name).
				Msg("found SearchConfig in operator provider workspace")
			return cfg
		}
		if !apierrors.IsNotFound(err) {
			log.Warn().Err(err).
				Str("schema", br.Schema.Name).
				Msg("error fetching SearchConfig from operator provider workspace, falling back to heuristic")
		}
	}
	return nil
}

// applySearchConfig classifies allFields according to the SearchConfig's declared lists.
// Priority: excludedFields > exactFields > semanticFields > default (full-text).
func applySearchConfig(allFields []string, cfg *pmsearchv1alpha1.SearchConfig) *searchIndexFields {
	excluded := toSet(cfg.Spec.ExcludedFields)
	semantic := toSet(cfg.Spec.SemanticFields)
	exact := toSet(cfg.Spec.ExactFields)

	result := &searchIndexFields{}
	for _, f := range allFields {
		switch {
		case excluded[f]:
			// Skip — not indexed at all.
		case exact[f]:
			result.filterableFields = append(result.filterableFields, f)
		case semantic[f]:
			result.semanticFields = append(result.semanticFields, f)
		default:
			result.defaultFields = append(result.defaultFields, f)
		}
	}

	// Also add semantic/exact fields that aren't in allFields (nested paths like "spec.description").
	for _, f := range cfg.Spec.SemanticFields {
		if !excluded[f] && !contains(allFields, f) {
			result.semanticFields = append(result.semanticFields, f)
		}
	}
	for _, f := range cfg.Spec.ExactFields {
		if !excluded[f] && !contains(allFields, f) {
			result.filterableFields = append(result.filterableFields, f)
		}
	}

	sort.Strings(result.defaultFields)
	sort.Strings(result.semanticFields)
	sort.Strings(result.filterableFields)
	return result
}

func toSet(slice []string) map[string]bool {
	m := make(map[string]bool, len(slice))
	for _, s := range slice {
		m[s] = true
	}
	return m
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// ensureSearchIndex creates or updates the SearchIndex in the org workspace.
// The resource is named after the derived index prefix so each binding gets its own SearchIndex.
func (s *apiBindingWatcherSubroutine) ensureSearchIndex(
	ctx context.Context,
	log *logger.Logger,
	orgName string,
	orgClusterID string,
	resource string,
	fields *searchIndexFields,
) error {
	orgsClient, err := buildWorkspaceScopedClient(s.rootCfg, s.mgr.GetLocalManager().GetScheme(), "root:orgs")
	if err != nil {
		return fmt.Errorf("build org client for %q: %w", orgName, err)
	}

	searchIndexName := buildCanonicalIndexName(s.indexPrefix, orgClusterID, resource)
	existing := &pmsearchv1alpha1.SearchIndex{}
	err = orgsClient.Get(ctx, types.NamespacedName{Name: searchIndexName}, existing)

	switch {
	case apierrors.IsNotFound(err):
		desired := &pmsearchv1alpha1.SearchIndex{
			ObjectMeta: metav1.ObjectMeta{
				Name: searchIndexName,
			},
			Spec: pmsearchv1alpha1.SearchIndexSpec{
				IndexPrefix:           sanitizeIndexNamePart(s.indexPrefix),
				OrganizationClusterID: orgClusterID,
				NumberOfShards:        1,
				NumberOfReplicas:      1,
				DefaultFields:         fields.defaultFields,
				SemanticFields:        fields.semanticFields,
				FilterableFields:      fields.filterableFields,
			},
		}
		if createErr := orgsClient.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("create SearchIndex %q in %q: %w", searchIndexName, orgName, createErr)
		}
		log.Info().
			Str("searchIndex", searchIndexName).
			Str("orgWorkspace", orgName).
			Int("defaultFields", len(fields.defaultFields)).
			Int("semanticFields", len(fields.semanticFields)).
			Int("filterableFields", len(fields.filterableFields)).
			Msg("created SearchIndex")

	case err != nil:
		return fmt.Errorf("get SearchIndex %q: %w", searchIndexName, err)

	default:
		if stringSlicesEqual(existing.Spec.DefaultFields, fields.defaultFields) &&
			stringSlicesEqual(existing.Spec.SemanticFields, fields.semanticFields) &&
			stringSlicesEqual(existing.Spec.FilterableFields, fields.filterableFields) {
			return nil
		}
		updated := existing.DeepCopy()
		updated.Spec.DefaultFields = fields.defaultFields
		updated.Spec.SemanticFields = fields.semanticFields
		updated.Spec.FilterableFields = fields.filterableFields
		if updateErr := orgsClient.Update(ctx, updated); updateErr != nil {
			if apierrors.IsConflict(updateErr) {
				return fmt.Errorf("conflict updating SearchIndex %q, will requeue: %w", searchIndexName, updateErr)
			}
			return fmt.Errorf("update SearchIndex %q in %q: %w", searchIndexName, orgName, updateErr)
		}
		log.Info().
			Str("searchIndex", searchIndexName).
			Str("orgWorkspace", orgName).
			Int("defaultFields", len(fields.defaultFields)).
			Int("semanticFields", len(fields.semanticFields)).
			Int("filterableFields", len(fields.filterableFields)).
			Msg("updated SearchIndex fields")
	}

	return nil
}

// stringSlicesEqual returns true when a and b contain the same elements in the same order.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
