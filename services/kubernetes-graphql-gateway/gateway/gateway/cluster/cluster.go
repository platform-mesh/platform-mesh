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

package cluster

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/roundtripper"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/roundtripper/union"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Cluster struct {
	name     string
	client   ctrlruntimeclient.WithWatch
	restCfg  *rest.Config
	adminCfg *rest.Config
}

// New creates a new Cluster connection from cluster metadata.
func New(
	ctx context.Context,
	name string,
	metadata *pmgatewayv1alpha1.ClusterMetadata,
) (*Cluster, error) {
	if metadata == nil {
		return nil, fmt.Errorf("cluster %s requires cluster metadata", name)
	}

	cluster := &Cluster{
		name: name,
	}

	var err error
	cluster.restCfg, err = pmgatewayv1alpha1.BuildRestConfigFromMetadata(*metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to build config from metadata: %w", err)
	}

	cluster.adminCfg = rest.CopyConfig(cluster.restCfg)

	basePath := hostPath(metadata.Host)
	tpl := metadata.RequestPathTemplate

	cluster.adminCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return roundtripper.NewPathTemplateHandler(rt, tpl, basePath)
	})

	tlsConfig := cluster.restCfg.TLSClientConfig
	baseRT, err := roundtripper.NewBaseRoundTripper(tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create base roundtripper: %w", err)
	}

	dataPlanePrefix := basePath + tpl
	cluster.restCfg.Wrap(func(adminRT http.RoundTripper) http.RoundTripper {
		return union.New(
			roundtripper.NewDiscoveryHandler(roundtripper.NewPathTemplateHandler(adminRT, dataPlanePrefix, basePath)),
			roundtripper.NewBearerHandler(roundtripper.NewPathTemplateHandler(baseRT, dataPlanePrefix, basePath), roundtripper.NewUnauthorizedRoundTripper()),
		)
	})

	var mapper meta.RESTMapper
	if metadata.IntrospectionPath != "" {
		mapper, err = restMapperFromConfig(cluster.adminCfg, metadata.IntrospectionPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create REST mapper: %w", err)
		}
	}

	cluster.client, err = ctrlruntimeclient.NewWithWatch(cluster.restCfg, ctrlruntimeclient.Options{Mapper: mapper})
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster client: %w", err)
	}

	logger := log.FromContext(ctx)
	logger.V(4).Info("Connected to cluster", "cluster", name)

	return cluster, nil
}

func (c *Cluster) Client() ctrlruntimeclient.WithWatch {
	return c.client
}

// RestConfig returns a copy of the cluster's rest.Config with the full
// roundtripper chain preserved, suitable for building typed clientsets.
func (c *Cluster) RestConfig() *rest.Config {
	return rest.CopyConfig(c.restCfg)
}

// AdminConfig returns a rest.Config with the cluster's admin credentials,
// suitable for privileged API calls like TokenReview.
func (c *Cluster) AdminConfig() *rest.Config {
	return rest.CopyConfig(c.adminCfg)
}

func (c *Cluster) Close() {
	c.client = nil
	c.adminCfg = nil
	c.restCfg = nil
}

func hostPath(host string) string {
	u, err := url.Parse(host)
	if err != nil {
		return ""
	}
	return strings.TrimRight(u.Path, "/")
}

func restMapperFromConfig(cfg *rest.Config, introspectionPath string) (meta.RESTMapper, error) {
	discoveryCfg := rest.CopyConfig(cfg)
	discoveryCfg.Host += introspectionPath

	httpClient, err := rest.HTTPClientFor(discoveryCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client for discovery: %w", err)
	}
	return apiutil.NewDynamicRESTMapper(discoveryCfg, httpClient)
}
