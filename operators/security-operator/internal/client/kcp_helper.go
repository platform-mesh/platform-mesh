package client

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/pkg/provider"
)

type KCPClientGetter interface {
	NewClientForLogicalCluster(ctx context.Context, cluster string) (client.Client, error)
	NewClientFromContext(ctx context.Context) (client.Client, error)
}

type Lister interface {
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

type KCPCombinedClientGetter interface {
	KCPClientGetter
	Lister
}

// ManagerKCPClientGetter retrieves cluster clients via the manager and builds
// all-Clients via the manager's config and scheme.
type ManagerKCPClientGetter struct {
	mgr      mcmanager.Manager
	provider *provider.Provider
}

func NewManagerKCPClientGetter(mgr mcmanager.Manager, provider *provider.Provider) *ManagerKCPClientGetter {
	return &ManagerKCPClientGetter{mgr: mgr, provider: provider}
}

func (f *ManagerKCPClientGetter) NewClientForLogicalCluster(ctx context.Context, cluster string) (client.Client, error) {
	kcpCluster, err := f.mgr.GetCluster(ctx, multicluster.ClusterName(cluster))
	if err != nil {
		return nil, fmt.Errorf("getting cluster: %w", err)
	}

	return kcpCluster.GetClient(), nil
}

func (f *ManagerKCPClientGetter) NewClientFromContext(ctx context.Context) (client.Client, error) {
	cl, err := f.mgr.ClusterFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting cluster from context: %w", err)
	}

	return cl.GetClient(), nil
}

func (f *ManagerKCPClientGetter) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return f.provider.Lister().List(ctx, list, opts...)
}

type ProviderLister struct {
	provider *provider.Provider
}

func NewProviderLister(provider *provider.Provider) *ProviderLister {
	return &ProviderLister{provider: provider}
}

// List lists resources across all clusters on all cshards.
func (p *ProviderLister) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return p.provider.Lister().List(ctx, list, opts...)
}

// ConfigSchemeKCPClientGetter builds cluster and all-Clients via a given config
// and scheme.
type ConfigSchemeKCPClientGetter struct {
	config *rest.Config
	scheme *runtime.Scheme
}

func NewConfigSchemeKCPClientGetter(config *rest.Config, scheme *runtime.Scheme) *ConfigSchemeKCPClientGetter {
	return &ConfigSchemeKCPClientGetter{
		config: config,
		scheme: scheme,
	}
}

func (f *ConfigSchemeKCPClientGetter) NewClientForLogicalCluster(ctx context.Context, cluster string) (client.Client, error) {
	_ = ctx
	return NewForLogicalCluster(f.config, f.scheme, logicalcluster.Name(cluster))
}

func (f *ConfigSchemeKCPClientGetter) NewClientFromContext(ctx context.Context) (client.Client, error) {
	clusterName, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("no cluster set in context, use ReconcilerWithCluster helper when building the controller")
	}

	return NewForLogicalCluster(f.config, f.scheme, logicalcluster.Name(clusterName))
}
