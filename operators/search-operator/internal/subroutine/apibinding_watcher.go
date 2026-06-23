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

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	lifecyclesubroutine "go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"

	"go.platform-mesh.io/apis/search/v1alpha1"
	"go.platform-mesh.io/search-operator/internal/metrics"
)

// apiBindingWatcherSubroutine watches APIBinding resources across workspaces.
// When a binding takes place in an org then all indexes are updated for the
// fields contained in the bound APIResourceSchemas.
type apiBindingWatcherSubroutine struct {
	mgr         mcmanager.Manager
	orgsClient  client.Client // scoped to root:orgs for Workspace lookups
	rootCfg     *rest.Config  // clean base kcp REST config (no path) for building workspace clients
	indexPrefix string
}

// NewAPIBindingWatcherSubroutine creates a new APIBinding watcher subroutine.
// orgsClient must be scoped to the root:orgs workspace.
// localCfg must be the admin kcp REST config.
func NewAPIBindingWatcherSubroutine(mgr mcmanager.Manager, orgsClient client.Client, localCfg *rest.Config, indexPrefix string) (*apiBindingWatcherSubroutine, error) {
	rootCfg, err := stripPathFromConfig(localCfg)
	if err != nil {
		return nil, err
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
// APIBinding, with DefaultFields populated from the top-level fields of all bound
// APIResourceSchemas.
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

	defaultFields, err := s.resolveDefaultFields(ctx, binding)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("resolve default fields for binding %q: %w", binding.Name, err), true, false)
	}

	for _, br := range binding.Status.AppliedPermissionClaims {
		if err := s.ensureSearchIndex(ctx, log, orgName, orgClusterID, br.Resource, defaultFields); err != nil {
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

// resolveDefaultFields collects the top-level field names from every APIResourceSchema
// referenced by the binding. Fields are returned unique in sorted order.
func (s *apiBindingWatcherSubroutine) resolveDefaultFields(ctx context.Context, binding *kcpapisv1alpha1.APIBinding) ([]string, error) {
	if len(binding.Status.BoundResources) == 0 {
		return nil, nil
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

	fields := make([]string, 0, len(seen))
	for f := range seen {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	return fields, nil
}

// ensureSearchIndex creates or updates the SearchIndex in the org workspace.
// The resource is named after the derived index prefix so each binding gets its own SearchIndex.
// TODO: maybe add a timestamp to avoid multiple edits of the SearchIndex if the
// APIResourceSchemas change and updates all bindings in an org
func (s *apiBindingWatcherSubroutine) ensureSearchIndex(
	ctx context.Context,
	log *logger.Logger,
	orgName string,
	orgClusterID string,
	resource string,
	defaultFields []string,
) error {
	orgsClient, err := buildWorkspaceScopedClient(s.rootCfg, s.mgr.GetLocalManager().GetScheme(), "root:orgs")
	if err != nil {
		return fmt.Errorf("build org client for %q: %w", orgName, err)
	}

	searchIndexName := buildCanonicalIndexName(s.indexPrefix, orgClusterID, resource)
	existing := &v1alpha1.SearchIndex{}
	err = orgsClient.Get(ctx, types.NamespacedName{Name: searchIndexName}, existing)

	switch {
	case apierrors.IsNotFound(err):
		desired := &v1alpha1.SearchIndex{
			ObjectMeta: metav1.ObjectMeta{
				Name: searchIndexName,
			},
			Spec: v1alpha1.SearchIndexSpec{
				IndexPrefix:           sanitizeIndexNamePart(s.indexPrefix),
				OrganizationClusterID: orgClusterID,
				NumberOfShards:        1,
				NumberOfReplicas:      1,
				DefaultFields:         defaultFields,
			},
		}
		if createErr := orgsClient.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("create SearchIndex %q in %q: %w", searchIndexName, orgName, createErr)
		}
		log.Info().
			Str("searchIndex", searchIndexName).
			Str("orgWorkspace", orgName).
			Int("defaultFields", len(defaultFields)).
			Msg("created SearchIndex")

	case err != nil:
		return fmt.Errorf("get SearchIndex %q: %w", searchIndexName, err)

	default:
		if stringSlicesEqual(existing.Spec.DefaultFields, defaultFields) {
			return nil
		}
		updated := existing.DeepCopy()
		updated.Spec.DefaultFields = defaultFields
		if updateErr := orgsClient.Update(ctx, updated); updateErr != nil {
			if apierrors.IsConflict(updateErr) {
				return fmt.Errorf("conflict updating SearchIndex %q, will requeue: %w", searchIndexName, updateErr)
			}
			return fmt.Errorf("update SearchIndex %q in %q: %w", searchIndexName, orgName, updateErr)
		}
		log.Info().
			Str("searchIndex", searchIndexName).
			Str("orgWorkspace", orgName).
			Int("defaultFields", len(defaultFields)).
			Msg("updated SearchIndex default fields")
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
