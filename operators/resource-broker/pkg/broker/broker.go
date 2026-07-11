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

// Package broker wires the resource broker controllers into a single
// multicluster manager.
package broker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"

	"go.platform-mesh.io/resource-broker/pkg/controller/broker/acceptapi"
	"go.platform-mesh.io/resource-broker/pkg/controller/coordbroker/migration"
	"go.platform-mesh.io/resource-broker/pkg/controller/coordbroker/stagingworkspace"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	"sigs.k8s.io/multicluster-runtime/providers/multi"
	"sigs.k8s.io/multicluster-runtime/providers/single"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

const (
	// AcceptAPIProviderName is the multi-provider name under which the
	// AcceptAPI virtual workspace is engaged.
	AcceptAPIProviderName = "acceptapi"

	// CoordinationClusterName is the multi-provider name under which the
	// coordination workspace is engaged.
	CoordinationClusterName = "coordination"

	// clusterNameSeparator separates the provider name from the cluster name
	// in multi-provider cluster names.
	clusterNameSeparator = "#"
)

// Options configures the broker.
type Options struct {
	// Log is the logger used by the broker.
	Log logr.Logger

	// LocalConfig is the config for the cluster hosting the broker manager.
	// Required.
	LocalConfig *rest.Config

	// KcpConfig is the config for the kcp workspace holding the broker's
	// APIExportEndpointSlices. It is also the base for workspace-scoped
	// clients.
	// Required.
	KcpConfig *rest.Config

	// ComputeConfig is the config for the compute cluster migrations deploy
	// their stage templates into.
	// Required.
	ComputeConfig *rest.Config

	// AcceptAPIName is the name of the APIExportEndpointSlice serving
	// AcceptAPIs. All other APIExportEndpointSlices in the broker workspace
	// serve brokered resources.
	// Required.
	AcceptAPIName string

	// CoordinationWorkspace is the kcp workspace path holding Assignments
	// and StagingWorkspaces.
	// Required.
	CoordinationWorkspace string

	// VerificationTreeRoot is the workspace path under which verification
	// workspaces are created.
	// Required.
	VerificationTreeRoot string

	// StagingTreeRoot is the workspace path under which staging workspaces
	// are created.
	// Required.
	StagingTreeRoot string

	// StageNamespace is the namespace in the compute cluster migrations
	// deploy their stage templates into.
	// Defaults to the migration controller's default.
	StageNamespace string

	// SkipNameValidation disables controller name uniqueness validation.
	// Set when running multiple brokers in one process, e.g. in tests.
	SkipNameValidation *bool

	// RequeueInterval is the requeue interval passed to all controllers.
	// Defaults to each controller's default.
	RequeueInterval time.Duration
}

func (opts *Options) validate() error {
	if opts.LocalConfig == nil {
		return errors.New("options: LocalConfig is required")
	}
	if opts.KcpConfig == nil {
		return errors.New("options: KcpConfig is required")
	}
	if opts.ComputeConfig == nil {
		return errors.New("options: ComputeConfig is required")
	}
	if opts.AcceptAPIName == "" {
		return errors.New("options: AcceptAPIName is required")
	}
	if opts.CoordinationWorkspace == "" {
		return errors.New("options: CoordinationWorkspace is required")
	}
	if opts.VerificationTreeRoot == "" {
		return errors.New("options: VerificationTreeRoot is required")
	}
	if opts.StagingTreeRoot == "" {
		return errors.New("options: StagingTreeRoot is required")
	}
	return nil
}

// Broker runs the resource broker manager.
type Broker struct {
	log logr.Logger
	mgr mcmanager.Manager
}

// New validates opts and wires all broker controllers.
func New(opts Options) (*Broker, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	scheme, err := newScheme()
	if err != nil {
		return nil, fmt.Errorf("building scheme: %w", err)
	}

	wcf, err := workspaceClientFunc(opts.KcpConfig, scheme)
	if err != nil {
		return nil, fmt.Errorf("building workspace client factory: %w", err)
	}

	multiProvider := multi.New(multi.Options{})

	mgr, err := newManager(opts.LocalConfig, scheme, multiProvider, opts.SkipNameValidation)
	if err != nil {
		return nil, fmt.Errorf("creating manager: %w", err)
	}

	b := &Broker{log: opts.Log, mgr: mgr}

	acceptAPIProvider, err := setupAcceptAPI(mgr, multiProvider, opts, scheme, wcf)
	if err != nil {
		return nil, fmt.Errorf("setting up acceptapi controller: %w", err)
	}

	if err := setupCoordination(mgr, multiProvider, opts, scheme, wcf); err != nil {
		return nil, fmt.Errorf("setting up coordination controllers: %w", err)
	}

	if err := setupDiscovery(mgr, multiProvider, opts, scheme, wcf, acceptAPIProvider); err != nil {
		return nil, fmt.Errorf("setting up discovery controller: %w", err)
	}

	return b, nil
}

// Start runs the manager until ctx is cancelled or it fails.
func (b *Broker) Start(ctx context.Context) error {
	return b.mgr.Start(ctx)
}

// providerClusters returns a cluster filter matching only clusters engaged by
// the named multi-provider entry.
func providerClusters(name string) mcbuilder.ClusterFilterFunc {
	prefix := name + clusterNameSeparator
	return func(clusterName multicluster.ClusterName, _ cluster.Cluster) bool {
		return strings.HasPrefix(string(clusterName), prefix)
	}
}

// setupAcceptAPI wires the AcceptAPI reconciler on the AcceptAPI virtual
// workspace and returns the provider for listing AcceptAPIs.
func setupAcceptAPI(mgr mcmanager.Manager, multiProvider *multi.Provider, opts Options, scheme *runtime.Scheme, wcf workspaceClientFn) (*apiexport.Provider, error) {
	provider, err := apiexport.New(opts.KcpConfig, opts.AcceptAPIName, apiexport.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("creating acceptapi provider: %w", err)
	}
	if err := multiProvider.AddProvider(AcceptAPIProviderName, provider); err != nil {
		return nil, fmt.Errorf("adding acceptapi provider: %w", err)
	}

	reconciler, err := acceptapi.NewReconciler(mgr, acceptapi.Options{
		VerificationTreeRoot: opts.VerificationTreeRoot,
		WorkspaceClientFunc:  wcf,
		ClusterFilter:        providerClusters(AcceptAPIProviderName),
		RequeueInterval:      opts.RequeueInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("creating acceptapi reconciler: %w", err)
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("setting up acceptapi reconciler: %w", err)
	}

	return provider, nil
}

// setupCoordination wires the Assignment and StagingWorkspace reconcilers on
// the coordination workspace.
func setupCoordination(mgr mcmanager.Manager, multiProvider *multi.Provider, opts Options, scheme *runtime.Scheme, wcf workspaceClientFn) error {
	coordConfig, err := configForClusterPath(opts.KcpConfig, opts.CoordinationWorkspace)
	if err != nil {
		return fmt.Errorf("building coordination config: %w", err)
	}

	coordCluster, err := cluster.New(coordConfig, func(o *cluster.Options) { o.Scheme = scheme })
	if err != nil {
		return fmt.Errorf("creating coordination cluster: %w", err)
	}

	if err := multiProvider.AddProvider(CoordinationClusterName, single.New(CoordinationClusterName, coordCluster)); err != nil {
		return fmt.Errorf("adding coordination provider: %w", err)
	}
	if err := mgr.GetLocalManager().Add(manager.RunnableFunc(coordCluster.Start)); err != nil {
		return fmt.Errorf("adding coordination cluster runnable: %w", err)
	}

	filter := providerClusters(CoordinationClusterName)

	stagingReconciler, err := stagingworkspace.NewReconciler(mgr, stagingworkspace.Options{
		StagingTreeRoot:     opts.StagingTreeRoot,
		WorkspaceClientFunc: wcf,
		ClusterFilter:       filter,
		RequeueInterval:     opts.RequeueInterval,
	})
	if err != nil {
		return fmt.Errorf("creating stagingworkspace reconciler: %w", err)
	}
	if err := stagingReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up stagingworkspace reconciler: %w", err)
	}

	computeClient, err := ctrlruntimeclient.New(opts.ComputeConfig, ctrlruntimeclient.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("building compute client: %w", err)
	}

	migrationReconciler, err := migration.NewReconciler(mgr, migration.Options{
		ComputeClient:       computeClient,
		WorkspaceClientFunc: wcf,
		StagingTreeRoot:     opts.StagingTreeRoot,
		StageNamespace:      opts.StageNamespace,
		ClusterFilter:       filter,
		RequeueInterval:     opts.RequeueInterval,
	})
	if err != nil {
		return fmt.Errorf("creating migration reconciler: %w", err)
	}
	if err := migrationReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up migration reconciler: %w", err)
	}

	return nil
}

// setupDiscovery wires the discovery controller watching
// APIExportEndpointSlices in the broker workspace. Each slice except the
// AcceptAPI one gets a provider and controllers for its brokered resources.
func setupDiscovery(mgr mcmanager.Manager, multiProvider *multi.Provider, opts Options, scheme *runtime.Scheme, wcf workspaceClientFn, acceptAPIProvider *apiexport.Provider) error {
	coordinationClient, err := wcf(opts.CoordinationWorkspace)
	if err != nil {
		return fmt.Errorf("building coordination client: %w", err)
	}

	kcpCluster, err := cluster.New(opts.KcpConfig, func(o *cluster.Options) { o.Scheme = scheme })
	if err != nil {
		return fmt.Errorf("creating kcp workspace cluster: %w", err)
	}
	if err := mgr.GetLocalManager().Add(manager.RunnableFunc(kcpCluster.Start)); err != nil {
		return fmt.Errorf("adding kcp workspace cluster runnable: %w", err)
	}

	disc := &discovery{
		log:           opts.Log.WithName("discovery"),
		client:        kcpCluster.GetClient(),
		kcpConfig:     opts.KcpConfig,
		scheme:        scheme,
		acceptAPIName: opts.AcceptAPIName,
		wcf:           wcf,
		providers:     multiProvider,
		register:      registerBrokeredResource(mgr, opts, wcf, coordinationClient, acceptAPIProvider),
	}

	slicesForExport := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj ctrlruntimeclient.Object) []reconcile.Request {
		slices := &kcpapisv1alpha1.APIExportEndpointSliceList{}
		if err := disc.client.List(ctx, slices); err != nil {
			disc.log.Error(err, "listing APIExportEndpointSlices")
			return nil
		}
		var reqs []reconcile.Request
		for _, slice := range slices.Items {
			if slice.Spec.APIExport.Name == obj.GetName() {
				reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: slice.Name}})
			}
		}
		return reqs
	})

	if err := ctrl.NewControllerManagedBy(mgr.GetLocalManager()).
		Named("discovery").
		WatchesRawSource(source.Kind(kcpCluster.GetCache(), ctrlruntimeclient.Object(&kcpapisv1alpha1.APIExportEndpointSlice{}), &handler.EnqueueRequestForObject{})).
		WatchesRawSource(source.Kind(kcpCluster.GetCache(), ctrlruntimeclient.Object(&kcpapisv1alpha2.APIExport{}), slicesForExport)).
		Complete(disc); err != nil {
		return fmt.Errorf("setting up discovery controller: %w", err)
	}

	return nil
}
