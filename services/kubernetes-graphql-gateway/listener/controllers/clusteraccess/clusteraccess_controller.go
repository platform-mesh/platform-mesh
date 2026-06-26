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

package clusteraccess

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/controllers/reconciler"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/apischema"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/apischema/enricher"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/schemahandler"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

const (
	controllerName = "clusteraccess-schema-controller"

	// ConditionTypeReady indicates whether the ClusterAccess schema was
	// successfully generated and written.
	ConditionTypeReady = "Ready"
)

var (
	ErrCreateHTTPClient = errors.New("failed to create HTTP client")
	ErrCreateRESTMapper = errors.New("failed to create REST mapper")
)

// ClusterAccessReconciler reconciles ClusterAccess resources and generates schemas
type ClusterAccessReconciler struct {
	manager   mcmanager.Manager
	opts      controller.TypedOptions[mcreconcile.Request]
	ioHandler schemahandler.Handler
}

// NewClusterAccessReconciler returns a new ClusterAccessReconciler
func NewClusterAccessReconciler(
	_ context.Context,
	mgr mcmanager.Manager,
	opts controller.TypedOptions[mcreconcile.Request],
	ioHandler schemahandler.Handler,
) (*ClusterAccessReconciler, error) {
	r := &ClusterAccessReconciler{
		manager:   mgr,
		opts:      opts,
		ioHandler: ioHandler,
	}

	return r, nil
}

// Reconcile handles the ClusterAccess reconciliation
func (r *ClusterAccessReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Reconciling ClusterAccess", "name", req.Name, "cluster", req.ClusterName)

	cl, err := r.manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get client for cluster %q: %w", req.ClusterName, err)
	}

	// Strip multi-provider prefix (e.g. "single#single" → "single") for
	// downstream use in schema paths.
	clusterName := reconciler.ClusterName(req.ClusterName)

	c := cl.GetClient()

	ca := &pmgatewayv1alpha1.ClusterAccess{}
	if err := c.Get(ctx, ctrlruntimeclient.ObjectKey{Name: req.Name}, ca); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ClusterAccess resource not found, cleaning up schema", "name", req.Name)
			// Delete the schema file if ClusterAccess is deleted
			// Try both possible paths (resource name and path field)
			name := req.Name
			if clusterName != "" {
				name = fmt.Sprintf("%s-%s", clusterName, name)
			}
			if err := r.ioHandler.Delete(ctx, name); err != nil {
				logger.Error(err, "Failed to cleanup schema")
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get ClusterAccess: %w", err)
	}

	result, reconcileErr := r.reconcileClusterAccess(ctx, ca, c, cl.GetConfig(), clusterName)

	// Update the Ready status condition based on the reconciliation outcome
	if err := r.setReadyCondition(ctx, ca, c, reconcileErr); err != nil {
		logger.Error(err, "Failed to update status conditions", "clusterAccess", ca.Name)
	}

	return result, reconcileErr
}

// reconcileClusterAccess performs the core reconciliation: building the schema and writing it.
func (r *ClusterAccessReconciler) reconcileClusterAccess(
	ctx context.Context,
	ca *pmgatewayv1alpha1.ClusterAccess,
	c ctrlruntimeclient.Client,
	currentConfig *rest.Config,
	reqClusterName string,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Determine cluster name/path for the schema file
	clusterName := ca.GetName()
	if ca.Spec.Path != "" {
		clusterName = ca.Spec.Path
	}
	if reqClusterName != "" {
		clusterName = fmt.Sprintf("%s-%s", reqClusterName, clusterName)
	}

	// Build target cluster config from ClusterAccess spec
	targetConfig, err := buildTargetClusterConfig(ctx, *ca, c, currentConfig)
	if err != nil {
		logger.Error(err, "Failed to build target cluster config", "clusterAccess", ca.Name)
		return ctrl.Result{}, err
	}

	// Create discovery client for target cluster
	targetDiscovery, err := discovery.NewDiscoveryClientForConfig(targetConfig)
	if err != nil {
		logger.Error(err, "Failed to create discovery client", "clusterAccess", ca.Name)
		return ctrl.Result{}, err
	}

	// Create REST mapper for target cluster
	targetRM, err := r.restMapperFromConfig(targetConfig)
	if err != nil {
		logger.Error(err, "Failed to create REST mapper", "clusterAccess", ca.Name)
		return ctrl.Result{}, err
	}

	// Get preferred resources for categories enricher
	apiResources, err := targetDiscovery.ServerPreferredResources()
	if err != nil {
		// Log but don't fail - some resources may still be available
		logger.V(2).Info("partial error getting server preferred resources", "error", err)
		if apiResources == nil {
			return ctrl.Result{}, fmt.Errorf("failed to get server preferred resources: %w", err)
		}
	}

	// Create resolver with enrichers configured for this cluster
	resolver := apischema.NewResolver(
		enricher.NewScope(targetRM),
		enricher.NewCategories(apiResources),
	)

	// Resolve schema from target cluster
	schemaJSON, err := resolver.Resolve(ctx, targetDiscovery.OpenAPIV3())
	if err != nil {
		logger.Error(err, "Failed to resolve schema", "clusterAccess", ca.Name)
		return ctrl.Result{}, err
	}

	// Inject cluster metadata into the schema
	schemaWithMetadata, err := injectClusterMetadata(ctx, schemaJSON, *ca, c, currentConfig)
	if err != nil {
		logger.Error(err, "Failed to inject cluster metadata", "clusterAccess", ca.Name)
		return ctrl.Result{}, err
	}

	// Write schema to file
	if err := r.ioHandler.Write(ctx, schemaWithMetadata, clusterName); err != nil {
		logger.Error(err, "Failed to write schema", "clusterAccess", ca.Name)
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled schema for ClusterAccess", "name", ca.Name, "path", clusterName)

	// If using SA auth, schedule requeue before token expires
	if ca.Spec.Auth != nil && ca.Spec.Auth.ServiceAccountRef != nil {
		requeueDuration := calculateTokenRefreshInterval(ca.Spec.Auth.ServiceAccountRef.TokenExpiration)
		logger.Info("Scheduled token refresh", "name", ca.Name, "requeueAfter", requeueDuration)
		return ctrl.Result{RequeueAfter: requeueDuration}, nil
	}

	return ctrl.Result{}, nil
}

// calculateTokenRefreshInterval returns when to requeue for token refresh.
// Requeues at 80% of token lifetime to refresh before expiry.
func calculateTokenRefreshInterval(tokenExpiration *metav1.Duration) time.Duration {
	const defaultExpiration = 1 * time.Hour

	var expiration time.Duration
	if tokenExpiration != nil && tokenExpiration.Duration > 0 {
		expiration = tokenExpiration.Duration
	} else {
		expiration = defaultExpiration
	}

	// Refresh at 80% of token lifetime (20% safety buffer)
	return time.Duration(float64(expiration) * 0.8)
}

// SetupWithManager sets up the controller with the Manager
func (r *ClusterAccessReconciler) SetupWithManager(mgr mcmanager.Manager, forOpts ...mcbuilder.ForOption) error {
	return mcbuilder.ControllerManagedBy(mgr).
		For(&pmgatewayv1alpha1.ClusterAccess{}, forOpts...).
		WithOptions(r.opts).
		Named(controllerName).
		Complete(r)
}

func buildTargetClusterConfig(ctx context.Context, clusterAccess pmgatewayv1alpha1.ClusterAccess, c ctrlruntimeclient.Client, currentConfig *rest.Config) (*rest.Config, error) {
	spec := clusterAccess.Spec

	config, err := pmgatewayv1alpha1.BuildRestConfigFromClusterAccess(ctx, clusterAccess, c)
	if err != nil {
		return nil, err
	}

	if len(config.CAData) == 0 && currentConfig != nil && len(currentConfig.CAData) > 0 {
		config.CAData = currentConfig.CAData
		config.Insecure = false
	}

	if spec.Host != "" {
		config.Host = spec.Host
	}

	if spec.IntrospectionPath != "" {
		config.Host += spec.IntrospectionPath
	}

	return config, nil
}

func injectClusterMetadata(ctx context.Context, schemaData []byte, clusterAccess pmgatewayv1alpha1.ClusterAccess, c ctrlruntimeclient.Client, currentConfig *rest.Config) ([]byte, error) {
	metadata, err := pmgatewayv1alpha1.BuildClusterMetadataFromClusterAccess(ctx, clusterAccess, c)
	if err != nil {
		return nil, fmt.Errorf("failed to build cluster metadata from ClusterAccess: %w", err)
	}

	if metadata.CA == nil && currentConfig != nil && len(currentConfig.CAData) > 0 {
		metadata.CA = &pmgatewayv1alpha1.CAMetadata{
			Data: base64.StdEncoding.EncodeToString(currentConfig.CAData),
		}
	}

	var schemaJSON map[string]any
	if err := json.Unmarshal(schemaData, &schemaJSON); err != nil {
		return nil, fmt.Errorf("failed to parse schema JSON: %w", err)
	}

	schemaJSON["x-cluster-metadata"] = metadata

	return json.Marshal(schemaJSON)
}

// restMapperFromConfig creates a REST mapper from a config
func (r *ClusterAccessReconciler) restMapperFromConfig(cfg *rest.Config) (meta.RESTMapper, error) {
	httpClt, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, errors.Join(ErrCreateHTTPClient, err)
	}
	rm, err := apiutil.NewDynamicRESTMapper(cfg, httpClt)
	if err != nil {
		return nil, errors.Join(ErrCreateRESTMapper, err)
	}

	return rm, nil
}

// setReadyCondition updates the Ready status condition on the ClusterAccess resource.
// On success (reconcileErr == nil) it sets Ready=True; on failure it sets Ready=False
// with the error message. This is best-effort: failures to update are returned but
// should not override the original reconciliation error.
func (r *ClusterAccessReconciler) setReadyCondition(ctx context.Context, ca *pmgatewayv1alpha1.ClusterAccess, c ctrlruntimeclient.Client, reconcileErr error) error {
	condition := metav1.Condition{
		Type:               ConditionTypeReady,
		ObservedGeneration: ca.Generation,
	}

	if reconcileErr == nil {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "ReconcileSucceeded"
		condition.Message = "Schema generated and written successfully"
	} else {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ReconcileFailed"
		condition.Message = reconcileErr.Error()
	}

	meta.SetStatusCondition(&ca.Status.Conditions, condition)

	return c.Status().Update(ctx, ca)
}
