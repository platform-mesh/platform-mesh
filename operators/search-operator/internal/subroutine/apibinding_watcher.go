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
	"slices"
	"time"

	pmsearchv1alpha1 "go.platform-mesh.io/apis/search/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/search-operator/internal/config"
	"go.platform-mesh.io/search-operator/internal/metrics"
	"go.platform-mesh.io/search-operator/internal/opensearch"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
)

// The apiBindingWatcherSubroutine only watches bindings of the Search export
type apiBindingWatcherSubroutine struct {
	mgr             mcmanager.Manager
	orgsClient      ctrlruntimeclient.Client
	rootCfg         *rest.Config
	cfg             config.OperatorConfig
	indexPrefix     string
	providerByGroup map[string]string
}

// NewAPIBindingWatcherSubroutine creates a new APIBinding watcher subroutine.
// orgsClient must be scoped to the root:orgs workspace.
// searchConfigClient must be scoped to the provider workspace.
// localCfg must be the admin kcp REST config.
func NewAPIBindingWatcherSubroutine(mgr mcmanager.Manager, orgsClient ctrlruntimeclient.Client, localCfg *rest.Config, indexPrefix string, cfg config.OperatorConfig) (lifecyclesubroutine.Subroutine, error) {
	rootCfg, err := stripPathFromConfig(localCfg)
	if err != nil {
		return nil, err
	}

	providerByGroup := make(map[string]string, len(cfg.SearchableResources))
	for _, rss := range cfg.SearchableResources {
		providerByGroup[rss.Group] = rss.Provider
	}

	return &apiBindingWatcherSubroutine{
		mgr:         mgr,
		orgsClient:  orgsClient,
		rootCfg:     rootCfg,
		indexPrefix: indexPrefix,
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

	wsClient, err := buildWorkspaceScopedClient(s.rootCfg, s.mgr.GetLocalManager().GetScheme(), workspacePath)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("build workspace client: %w", err), true, false)
	}
	otherBindings := &kcpapisv1alpha1.APIBindingList{}
	if err := wsClient.List(ctx, otherBindings); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("list APIBindings in workspace: %w", err), true, false)
	}

	orgName, err := extractOrgFromPath(workspacePath)
	if err != nil {
		log.Debug().Str("workspacePath", workspacePath).Msg("APIBinding is not in an org workspace, skipping")
		return ctrl.Result{}, nil
	}

	orgClusterID, err := getOrgClusterID(ctx, s.orgsClient, orgName)
	if err != nil {
		log.Debug().Err(err).Str("orgName", orgName).Msg("org Workspace not found, requeuing")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	for _, b := range otherBindings.Items {
		if b.Status.Phase != kcpapisv1alpha1.APIBindingPhaseBound {
			continue
		}
		mcMngr := s.mgr.GetLocalManager()

		providerPath := b.Spec.Reference.Export.Path
		providerClient, err := GetScopedClient(mcMngr.GetConfig(), mcMngr.GetScheme(), providerPath)
		if err != nil {
			log.Warn().Str("provider", providerPath).Msg("failed to get provider client")
			continue
		}

		schemas, err := boundSchemasForBinding(ctx, providerClient, b)
		if err != nil {
			log.Warn().Str("binding", b.Name).AnErr("due to", err).Msg("failed to get bound Resource Schemas for binding")
			continue
		}
		if len(schemas) == 0 {
			log.Debug().Str("provider", providerPath).Msg("no matching APIResourceSchemas found for binding")
			continue
		}

		for _, sc := range schemas {
			fields, err := s.resolveFieldsForSchema(ctx, providerClient, sc)
			if err != nil {
				return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("resolve fields for binding %q: %w", binding.Name, err), true, false)
			}
			// the current convention is using the resource plural for the index but ideally we use sc.Name and be consistent
			// everywhere else the index name is fetched.
			if err := s.ensureSearchIndex(ctx, log, orgName, orgClusterID, sc.Spec.Names.Plural, fields); err != nil {
				return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("ensure SearchIndex for binding %q resource %q: %w", binding.Name, sc.Name, err), true, false)
			}
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

// resolveFieldsForPermissionClaim finds the APIResourceSchema for the claimed (group, resource)
// by searching configured provider workspaces, then classifies fields using any SearchConfig
// found in the same provider workspace.
func (s *apiBindingWatcherSubroutine) resolveFieldsForSchema(ctx context.Context, pc ctrlruntimeclient.Client, rs *kcpapisv1alpha1.APIResourceSchema) (*searchIndexFields, error) {
	log := logger.LoadLoggerFromContext(ctx)

	defaultFields := sets.New[string]()
	semanticFields := sets.New[string]()
	exactFields := sets.New[string]()

	var cf *searchConfigFilter
	searchCfg := &pmsearchv1alpha1.SearchConfig{}
	if err := pc.Get(ctx, types.NamespacedName{Name: rs.Name}, searchCfg); err != nil {
		log.Warn().Str("schema", rs.Name).Msg("no SearchConfig found, all fields are default fields")
	} else {
		log.Debug().Str("searchConfig", searchCfg.Name).Str("schema", rs.Name).Msg("found SearchConfig in provider workspace")
		filter := newSearchConfigFilter(searchCfg)
		cf = &filter
	}

	for _, version := range rs.Spec.Versions {
		if !version.Served {
			continue
		}
		props, err := version.GetSchema()
		if err != nil {
			return nil, fmt.Errorf("parse schema for %q version %q: %w", rs.Name, version.Name, err)
		}
		if props == nil {
			continue
		}
		for _, fieldPath := range collectLeafFieldPaths(props) {
			if cf == nil {
				defaultFields.Insert(fieldPath)
			} else {
				cf.sortIntoAnyOf(fieldPath, defaultFields, semanticFields, exactFields)
			}
		}
	}

	// default filters that can always be extracted for each Resource
	exactFields.Insert(opensearch.DefaultFilterableFields...)

	return &searchIndexFields{
		defaultFields:    sets.List(defaultFields),
		semanticFields:   sets.List(semanticFields),
		filterableFields: sets.List(exactFields),
	}, nil
}

func boundSchemasForBinding(ctx context.Context, providerClient ctrlruntimeclient.Client, b kcpapisv1alpha1.APIBinding) ([]*kcpapisv1alpha1.APIResourceSchema, error) {
	schemaList := &kcpapisv1alpha1.APIResourceSchemaList{}
	if err := providerClient.List(ctx, schemaList); err != nil {
		return nil, err
	}
	boundNames := sets.New[string]()
	for _, br := range b.Status.BoundResources {
		boundNames.Insert(br.Schema.Name)
	}
	var matched []*kcpapisv1alpha1.APIResourceSchema
	for i := range schemaList.Items {
		if boundNames.Has(schemaList.Items[i].Name) {
			matched = append(matched, &schemaList.Items[i])
		}
	}
	return matched, nil
}

// maxSchemaWalkDepth bounds recursion into pathological / deeply nested schemas.
const maxSchemaWalkDepth = 10

// collectLeafFieldPaths walks the schema's Properties recursively and returns a dot-notation
// path for every leaf field. A leaf is any node with no child Properties, which includes
// scalars, arrays, and map-typed (additionalProperties) nodes. Arrays and maps are emitted as
// a single leaf path (they are not descended into), so the paths stay resolvable by the
// map-only lookupFieldPath used during indexing.
func collectLeafFieldPaths(root *apiextensionsv1.JSONSchemaProps) []string {
	var out []string
	var walk func(prefix string, node *apiextensionsv1.JSONSchemaProps, depth int)
	walk = func(prefix string, node *apiextensionsv1.JSONSchemaProps, depth int) {
		if node == nil {
			return
		}
		if depth > maxSchemaWalkDepth || len(node.Properties) == 0 {
			if prefix != "" {
				out = append(out, prefix)
			}
			return
		}
		for name := range node.Properties {
			child := node.Properties[name] // map values are not addressable; copy first
			path := name
			if prefix != "" {
				path = prefix + "." + name
			}
			walk(path, &child, depth+1)
		}
	}
	walk("", root, 0)
	return out
}

// searchConfigFilter holds pre-built sets from a SearchConfig for fast field classification.
type searchConfigFilter struct {
	excluded sets.Set[string]
	exact    sets.Set[string]
	semantic sets.Set[string]
}

func newSearchConfigFilter(cfg *pmsearchv1alpha1.SearchConfig) searchConfigFilter {
	return searchConfigFilter{
		excluded: sets.New(cfg.Spec.ExcludedFields...),
		exact:    sets.New(cfg.Spec.ExactFields...),
		semantic: sets.New(cfg.Spec.SemanticFields...),
	}
}

// sortIntoAnyOf classifies element by priority: excluded fields are dropped; otherwise it goes
// into exactFields if listed as exact, semanticFields if listed as semantic, else defaultFields.
func (f searchConfigFilter) sortIntoAnyOf(element string, defaultFields, semanticFields, exactFields sets.Set[string]) {
	if f.excluded.Has(element) {
		return
	}
	if f.exact.Has(element) {
		exactFields.Insert(element)
		return
	}
	if f.semantic.Has(element) {
		semanticFields.Insert(element)
		return
	}
	defaultFields.Insert(element)
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
		if slices.Equal(existing.Spec.DefaultFields, fields.defaultFields) &&
			slices.Equal(existing.Spec.SemanticFields, fields.semanticFields) &&
			slices.Equal(existing.Spec.FilterableFields, fields.filterableFields) {
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
