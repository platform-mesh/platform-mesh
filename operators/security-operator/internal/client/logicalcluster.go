package client

import (
	"fmt"
	"net/url"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"github.com/kcp-dev/logicalcluster/v3"
)

// NewForLogicalCluster returns a client for a given logical cluster name or
// path, based on a KCP base config.
func NewForLogicalCluster(config *rest.Config, scheme *runtime.Scheme, clusterKey logicalcluster.Name) (client.Client, error) {
	path := fmt.Sprintf("/clusters/%s", clusterKey)

	return clientForPath(config, scheme, path)
}

// clientForPath returns a client for a give raw URL path.
func clientForPath(config *rest.Config, scheme *runtime.Scheme, path string) (client.Client, error) {
	copy := rest.CopyConfig(config)

	parsed, err := url.Parse(copy.Host)
	if err != nil {
		return nil, fmt.Errorf("parsing host from config: %w", err)
	}
	parsed.Path = path
	copy.Host = parsed.String()

	return client.New(copy, client.Options{
		Scheme: scheme,
	})
}
