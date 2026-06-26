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

package resource

import (
	"context"
	"fmt"

	"github.com/google/cel-go/cel"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/controllers/reconciler"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/schemahandler"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

const (
	controllerName = "resource-schema-controller"
)

// Reconciler reconciles the anchor resource to trigger schema generation
type Reconciler struct {
	manager                     mcmanager.Manager
	opts                        controller.TypedOptions[mcreconcile.Request]
	reconciler                  *reconciler.Reconciler
	anchorResource              string
	resourceGVK                 schema.GroupVersionKind
	additionalPathAnnotationKey string

	// Provider specific functions
	clusterMetadataFunc    pmgatewayv1alpha1.ClusterMetadataFunc
	clusterURLResolverFunc pmgatewayv1alpha1.ClusterURLResolver
}

// New returns a new ResourceReconciler
func New(
	_ context.Context,
	mgr mcmanager.Manager,
	opts controller.TypedOptions[mcreconcile.Request],
	schemaHandler schemahandler.Handler,
	anchorResource string,
	resourceGVR string,
	additionalPathAnnotationKey string,
	clusterMetadataFunc pmgatewayv1alpha1.ClusterMetadataFunc,
	clusterURLResolverFunc pmgatewayv1alpha1.ClusterURLResolver,
) (*Reconciler, error) {
	r := &Reconciler{
		manager:                     mgr,
		opts:                        opts,
		reconciler:                  reconciler.NewReconciler(schemaHandler),
		anchorResource:              anchorResource,
		additionalPathAnnotationKey: additionalPathAnnotationKey,

		clusterMetadataFunc:    clusterMetadataFunc,
		clusterURLResolverFunc: clusterURLResolverFunc,
	}

	gvr, gr := schema.ParseResourceArg(resourceGVR)
	if gvr == nil {
		gvr = &schema.GroupVersionResource{
			Group:    "",
			Version:  gr.Group,
			Resource: gr.Resource,
		}
	}

	var err error
	r.resourceGVK, err = mgr.GetLocalManager().GetRESTMapper().KindFor(*gvr)
	if err != nil {
		return nil, fmt.Errorf("failed to get GVK for GVR %q: %w", gvr.String(), err)
	}

	return r, nil
}

// Reconcile handles the namespace reconciliation
func (r *Reconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Reconciling anchor resource", "resourceName", req.Name, "cluster", req.ClusterName)

	cl, err := r.manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get client for cluster %q: %w", req.ClusterName, err)
	}

	// Strip multi-provider prefix (e.g. "kcp#workspace1" → "workspace1") for
	// downstream use in URLs, schema paths, and metadata lookups.
	clusterName := reconciler.ClusterName(req.ClusterName)

	c := cl.GetClient()
	config := rest.CopyConfig(cl.GetConfig())

	config.Host, err = r.clusterURLResolverFunc(config.Host, clusterName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to resolve cluster URL: %w", err)
	}

	// If we are running in k8s mode, the cluster name might be empty.
	paths := []string{"default"}
	if clusterName != "" {
		paths = []string{clusterName}
	}

	us := unstructured.Unstructured{}
	us.SetGroupVersionKind(r.resourceGVK)

	// Check if the anchor resource exists
	if err := c.Get(ctx, req.NamespacedName, &us); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Anchor resource not found, cleaning up schema", "resource", r.anchorResource)
			// Delete the schema file if namespace is deleted
			if err := r.reconciler.Cleanup(ctx, paths); err != nil {
				logger.Error(err, "Failed to cleanup schema")
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get resource: %w", err)
	}

	if r.additionalPathAnnotationKey != "" && us.GetAnnotations() != nil {
		if additionalPath, ok := us.GetAnnotations()[r.additionalPathAnnotationKey]; ok {
			logger.V(4).Info("Found additional path annotation on anchor resource", "annotationKey", r.additionalPathAnnotationKey, "additionalPath", additionalPath)
			paths = append(paths, additionalPath)
		}
	}

	// This is plugable function to get cluster metadata for the given cluster name.
	var metadata *pmgatewayv1alpha1.ClusterMetadata
	if r.clusterMetadataFunc != nil {
		var err error
		metadata, err = r.clusterMetadataFunc(clusterName)
		if err != nil {
			logger.Error(err, "Failed to get cluster metadata for namespace reconciliation", "cluster", req.ClusterName)
			return ctrl.Result{}, fmt.Errorf("failed to get cluster metadata for namespace reconciliation: %w", err)
		}
	} else {
		var err error
		metadata, err = pmgatewayv1alpha1.BuildClusterMetadataFromConfig(config)
		if err != nil {
			logger.Error(err, "Failed to build metadata from config")
			return ctrl.Result{}, fmt.Errorf("failed to build metadata from config: %w", err)
		}
	}

	// Generate schema for the cluster
	if err := r.reconciler.Reconcile(ctx, paths, config, metadata); err != nil {
		logger.Error(err, "Failed to reconcile schema")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled schema for cluster")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *Reconciler) SetupWithManager(mgr mcmanager.Manager, forOpts ...mcbuilder.ForOption) error {
	env, err := cel.NewEnv(
		cel.Variable("object", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return fmt.Errorf("failed to create CEL environment: %w", err)
	}

	ast, issues := env.Compile(r.anchorResource)
	if issues != nil && issues.Err() != nil {
		return fmt.Errorf("failed to compile anchor resource CEL expression: %w", issues.Err())
	}

	if ast.OutputType() != cel.BoolType {
		return fmt.Errorf("anchor resource CEL expression must return a boolean, got: %s", ast.OutputType().String())
	}

	prg, err := env.Program(ast,
		cel.EvalOptions(cel.OptOptimize),
	)
	if err != nil {
		return fmt.Errorf("failed to create CEL program for anchor resource: %w", err)
	}

	// Create a predicate to only watch the anchor resource
	anchorResourcePredicate := predicate.NewPredicateFuncs(func(object ctrlruntimeclient.Object) bool {
		us, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
		if err != nil {
			klog.Error("failure converting object to unstructured", "err", err.Error())
			return false
		}

		// For now I decided to give it the whole object, so that more complex expressions can be built.
		out, _, err := prg.Eval(map[string]any{
			"object": us,
		})
		if err != nil {
			klog.Error("failure evaluating expression", "err", err.Error())
			return false
		}

		return out.Value().(bool)
	})

	us := unstructured.Unstructured{}
	us.SetGroupVersionKind(r.resourceGVK)

	opts := make([]mcbuilder.ForOption, 0, len(forOpts)+1)
	opts = append(opts, mcbuilder.WithPredicates(anchorResourcePredicate))
	opts = append(opts, forOpts...)

	return mcbuilder.ControllerManagedBy(mgr).
		For(&us, opts...).
		WithOptions(r.opts).
		Named(controllerName).
		Complete(r)
}
