package terminatingworkspaces

import (
	"strings"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	"github.com/kcp-dev/logicalcluster/v3"
	mcpcache "github.com/kcp-dev/multicluster-provider/pkg/cache"
	"github.com/kcp-dev/multicluster-provider/pkg/events/recorder"
	"github.com/kcp-dev/multicluster-provider/pkg/handlers"
	"github.com/kcp-dev/multicluster-provider/pkg/provider"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

var _ multicluster.Provider = &Provider{}
var _ multicluster.ProviderRunnable = &Provider{}

// Provider reconciles LogicalClusters that are in deletion and have a specific
// terminator.
// It is a slightly modified version of
// github.com/kcp-dev/multicluster-provider/initializingworkspaces.
type Provider struct {
	*provider.Provider
}

// Options are the options for creating a new instance of the terminating workspaces provider.
type Options struct {
	// Scheme is the scheme to use for the provider. If this is nil, it defaults
	// to the client-go scheme.
	Scheme *runtime.Scheme

	// Log is the logger to use for the provider. If this is nil, it defaults
	// to the controller-runtime default logger.
	Log *logr.Logger

	// ObjectToWatch is the object type that the provider watches via a /clusters/*
	// wildcard endpoint to extract information about logical clusters joining and
	// leaving the "fleet" of (logical) clusters in kcp. If this is nil, it defaults
	// to [kcpcorev1alpha1.LogicalCluster]. This might be useful when using this provider
	// against custom virtual workspaces that are not the APIExport one but share
	// the same endpoint semantics.
	ObjectToWatch client.Object

	// Handlers are lifecycle handlers, ran for each logical cluster in the provider represented
	// by LogicalCluster object.
	Handlers handlers.Handlers
}

// New creates a new kcp terminating workspaces provider.
func New(cfg *rest.Config, workspaceTypeName string, options Options) (*Provider, error) {
	if options.ObjectToWatch == nil {
		options.ObjectToWatch = &kcpcorev1alpha1.LogicalCluster{}
	}

	if options.Log == nil {
		options.Log = ptr.To(log.Log.WithName("kcp-terminatingworkspaces-cluster-provider"))
	}

	p, err := provider.NewProvider(cfg, workspaceTypeName, provider.Options{
		Scheme:              options.Scheme,
		EndpointSliceObject: &kcptenancyv1alpha1.WorkspaceType{},
		ExtractURLsFromEndpointSlice: func(obj client.Object) ([]string, error) {
			wst := obj.(*kcptenancyv1alpha1.WorkspaceType)
			var urls []string
			for _, endpoint := range wst.Status.VirtualWorkspaces {
				if !strings.Contains(endpoint.URL, "/terminatingworkspaces/") {
					continue
				}
				urls = append(urls, endpoint.URL)
			}
			return urls, nil
		},
		ObjectToWatch: options.ObjectToWatch,
		Log:           options.Log,
		Handlers:      options.Handlers,
		NewCluster: func(cfg *rest.Config, clusterName logicalcluster.Name, wildcardCA mcpcache.WildcardCache, scheme *runtime.Scheme, _ recorder.EventRecorderGetter) (*mcpcache.ScopedCluster, error) {
			return mcpcache.NewScopedInitializingCluster(cfg, clusterName, wildcardCA, scheme)
		},
	})
	if err != nil {
		return nil, err
	}

	return &Provider{Provider: p}, nil
}
