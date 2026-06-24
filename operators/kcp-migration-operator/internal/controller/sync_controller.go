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

package controller

import (
	"context"
	"strings"

	"github.com/rs/zerolog"

	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/kcp-migration-operator/internal/config"
	"go.platform-mesh.io/kcp-migration-operator/internal/kcp"
	"go.platform-mesh.io/kcp-migration-operator/internal/transform"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// SyncController watches source resources and syncs them to kcp workspaces
type SyncController struct {
	ctrlruntimeclient.Client
	Log                    *logger.Logger
	Config                 *config.SyncConfig
	WorkspaceClientFactory kcp.WorkspaceClientFactory
	gvk                    schema.GroupVersionKind
}

// NewSyncController creates a new SyncController
func NewSyncController(
	client ctrlruntimeclient.Client,
	log *logger.Logger,
	cfg *config.SyncConfig,
	workspaceFactory kcp.WorkspaceClientFactory,
) *SyncController {
	// Parse API version into group and version
	gv := parseAPIVersion(cfg.Source.APIVersion)
	gvk := schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    cfg.Source.Kind,
	}

	return &SyncController{
		Client:                 client,
		Log:                    log,
		Config:                 cfg,
		WorkspaceClientFactory: workspaceFactory,
		gvk:                    gvk,
	}
}

// parseAPIVersion parses "group/version" or "version" into GroupVersion
func parseAPIVersion(apiVersion string) schema.GroupVersion {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 1 {
		return schema.GroupVersion{Version: parts[0]}
	}
	return schema.GroupVersion{Group: parts[0], Version: parts[1]}
}

//+kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch

// Reconcile handles reconciliation of source resources for syncing to kcp
func (s *SyncController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := s.Log.With().
		Str("name", req.Name).
		Str("namespace", req.Namespace).
		Str("kind", s.Config.Source.Kind).
		Logger()

	log.Info().Msg("syncing resource to kcp")

	// Fetch the source resource
	source := &unstructured.Unstructured{}
	source.SetGroupVersionKind(s.gvk)

	if err := s.Get(ctx, req.NamespacedName, source); err != nil {
		if apierrors.IsNotFound(err) {
			// Resource was deleted - handle deletion from kcp
			log.Info().Msg("source resource not found, may have been deleted")
			// TODO: Implement deletion propagation to kcp
			return ctrl.Result{}, nil
		}
		log.Error().Err(err).Msg("failed to get source resource")
		return ctrl.Result{}, err
	}

	// Log resource details
	log.Info().
		Str("uid", string(source.GetUID())).
		Int64("generation", source.GetGeneration()).
		Str("targetWorkspaceExpression", s.Config.Target.WorkspaceExpression).
		Msg("found source resource")

	// Step 1: Evaluate target workspace expression using Go templates
	workspacePath, err := transform.EvaluateWorkspaceExpression(s.Config.Target.WorkspaceExpression, source)
	if err != nil {
		log.Error().Err(err).Msg("failed to evaluate workspace expression")
		return ctrl.Result{}, err
	}

	log.Info().Str("workspacePath", workspacePath).Msg("evaluated workspace path")

	// Step 2: Get kcp client for target workspace
	if s.WorkspaceClientFactory == nil {
		log.Warn().Msg("workspace client factory not configured, skipping sync to kcp")
		return ctrl.Result{}, nil
	}

	kcpClient, err := s.WorkspaceClientFactory.GetClient(workspacePath)
	if err != nil {
		log.Error().Err(err).Str("workspacePath", workspacePath).Msg("failed to get kcp client")
		return ctrl.Result{}, err
	}

	// Step 3: Prepare target resource - use template if configured, otherwise pass-through
	target, err := s.prepareTargetResource(source)
	if err != nil {
		log.Error().Err(err).Msg("failed to prepare target resource")
		return ctrl.Result{}, err
	}

	log.Info().
		Str("targetName", target.GetName()).
		Str("targetNamespace", target.GetNamespace()).
		Str("targetKind", target.GetKind()).
		Msg("prepared target resource")

	// Step 4: Create or update resource in target workspace
	if err := s.syncToKCP(ctx, kcpClient, target, log); err != nil {
		log.Error().Err(err).Msg("failed to sync resource to kcp")
		return ctrl.Result{}, err
	}

	log.Info().Msg("sync completed successfully")

	return ctrl.Result{}, nil
}

// prepareTargetResource creates a target resource for kcp from the source
// If a template is configured, it transforms the source using the template.
// Otherwise, it uses pass-through mode (copy source with cleaned metadata).
func (s *SyncController) prepareTargetResource(source *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	// If template is configured, use template transformation
	if s.Config.Transform.Template != "" {
		return transform.ApplyTemplate(source, s.Config.Transform.Template)
	}

	// Pass-through mode: copy source with cleaned metadata
	target := source.DeepCopy()

	// Clean up metadata that shouldn't be copied to target
	metadata := target.Object["metadata"].(map[string]any)

	// Remove cluster-specific fields
	delete(metadata, "uid")
	delete(metadata, "resourceVersion")
	delete(metadata, "creationTimestamp")
	delete(metadata, "managedFields")
	delete(metadata, "ownerReferences")
	delete(metadata, "finalizers")
	delete(metadata, "selfLink")
	delete(metadata, "generation")

	// If target namespace is specified in config, use it
	if s.Config.Target.Namespace != "" {
		metadata["namespace"] = s.Config.Target.Namespace
	}

	// Add annotation to track source
	annotations, ok := metadata["annotations"].(map[string]any)
	if !ok {
		annotations = make(map[string]any)
	}
	annotations["migration.platform-mesh.io/source-uid"] = string(source.GetUID())
	annotations["migration.platform-mesh.io/source-generation"] = source.GetGeneration()
	metadata["annotations"] = annotations

	return target, nil
}

// syncToKCP creates or updates the resource in kcp
func (s *SyncController) syncToKCP(ctx context.Context, kcpClient ctrlruntimeclient.Client, target *unstructured.Unstructured, log zerolog.Logger) error {
	// Try to get existing resource
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(target.GroupVersionKind())

	err := kcpClient.Get(ctx, ctrlruntimeclient.ObjectKey{
		Namespace: target.GetNamespace(),
		Name:      target.GetName(),
	}, existing)

	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new resource
			log.Info().Msg("creating resource in kcp")
			if err := kcpClient.Create(ctx, target); err != nil {
				return err
			}
			log.Info().Msg("created resource in kcp")
			return nil
		}
		return err
	}

	// Update existing resource
	// Set the resource version from existing for update
	target.SetResourceVersion(existing.GetResourceVersion())

	log.Info().Msg("updating resource in kcp")
	if err := kcpClient.Update(ctx, target); err != nil {
		if apierrors.IsConflict(err) {
			// Conflict - will be retried
			log.Info().Msg("conflict updating resource, will retry")
			return err
		}
		return err
	}
	log.Info().Msg("updated resource in kcp")
	return nil
}

// SetupWithManager sets up the controller with the Manager
func (s *SyncController) SetupWithManager(mgr ctrl.Manager) error {
	// Create an unstructured object for the source kind
	source := &unstructured.Unstructured{}
	source.SetGroupVersionKind(s.gvk)

	// Default to 1 worker if not configured
	maxWorkers := s.Config.Performance.MaxWorkers
	if maxWorkers < 1 {
		maxWorkers = 1
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(source).
		Named("sync").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxWorkers,
		})

	// Collect predicates
	var predicates []predicate.Predicate

	// Add namespace predicate if source namespace is configured
	if s.Config.Source.Namespace != "" {
		predicates = append(predicates, predicate.NewPredicateFuncs(func(obj ctrlruntimeclient.Object) bool {
			return obj.GetNamespace() == s.Config.Source.Namespace
		}))
	}

	// Add label selector predicates if configured
	if len(s.Config.Source.LabelSelectors) > 0 {
		labelPredicate, err := s.buildLabelSelectorPredicate()
		if err != nil {
			return err
		}
		predicates = append(predicates, labelPredicate)
	}

	// Apply all predicates with AND logic
	if len(predicates) > 0 {
		builder = builder.WithEventFilter(predicate.And(predicates...))
	}

	return builder.Complete(s)
}

// buildLabelSelectorPredicate creates a predicate that filters resources based on label selectors.
// All selectors must match (AND logic between selectors).
func (s *SyncController) buildLabelSelectorPredicate() (predicate.Predicate, error) {
	var selectors []labels.Selector

	for _, selectorStr := range s.Config.Source.LabelSelectors {
		selector, err := labels.Parse(selectorStr)
		if err != nil {
			s.Log.Error().Err(err).Str("selector", selectorStr).Msg("failed to parse label selector")
			return nil, err
		}
		selectors = append(selectors, selector)
	}

	s.Log.Info().
		Strs("labelSelectors", s.Config.Source.LabelSelectors).
		Msg("configured label selector filtering")

	return predicate.NewPredicateFuncs(func(obj ctrlruntimeclient.Object) bool {
		objLabels := labels.Set(obj.GetLabels())
		for _, selector := range selectors {
			if !selector.Matches(objLabels) {
				return false
			}
		}
		return true
	}), nil
}

var _ reconcile.Reconciler = &SyncController{}
