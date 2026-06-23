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
	"net/url"
	"strings"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpcore "github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// stripPathFromConfig returns a copy of cfg with the URL path cleared,
// leaving a clean base URL for workspace routing.
func stripPathFromConfig(cfg *rest.Config) (*rest.Config, error) {
	out := rest.CopyConfig(cfg)
	parsed, err := url.Parse(out.Host)
	if err != nil {
		return nil, fmt.Errorf("parse kcp host URL: %w", err)
	}
	parsed.Path = ""
	out.Host = parsed.String()
	return out, nil
}

// getWorkspaceClusterAndPath reads the LogicalCluster singleton from the
// current workspace and returns its cluster ID and path annotation
// (e.g. "root:orgs:acme").
func getWorkspaceClusterAndPath(ctx context.Context, mgr mcmanager.Manager) (clusterID multicluster.ClusterName, workspacePath string, err error) {
	id, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return "", "", fmt.Errorf("cluster not found in context")
	}

	cluster, err := mgr.GetCluster(ctx, id)
	if err != nil {
		return "", "", fmt.Errorf("failed to get cluster %q: %w", id, err)
	}

	// Use client.New directly — cluster.GetClient() is scoped to the APIExport
	// virtual workspace and cannot reach core.kcp.io resources.
	cl, err := client.New(cluster.GetConfig(), client.Options{Scheme: cluster.GetScheme()})
	if err != nil {
		return "", "", fmt.Errorf("failed to create client for cluster %q: %w", id, err)
	}

	lc := &kcpcorev1alpha1.LogicalCluster{}
	if err := cl.Get(ctx, client.ObjectKey{Name: kcpcorev1alpha1.LogicalClusterName}, lc); err != nil {
		return "", "", fmt.Errorf("failed to get LogicalCluster for %q: %w", id, err)
	}

	path, ok := lc.Annotations[kcpcore.LogicalClusterPathAnnotationKey]
	if !ok {
		return "", "", fmt.Errorf("LogicalCluster %q missing %s annotation", id, kcpcore.LogicalClusterPathAnnotationKey)
	}

	return id, path, nil
}

// extractOrgFromPath parses "root:orgs:acme[:...]" and returns "acme".
// Returns an error when the path is not under root:orgs.
func extractOrgFromPath(path string) (string, error) {
	parts := strings.Split(path, ":")
	if len(parts) < 3 || parts[0] != "root" || parts[1] != "orgs" {
		return "", fmt.Errorf("path %q is not under root:orgs", path)
	}
	return parts[2], nil
}

// getOrgClusterID returns the logical cluster ID for the named org workspace
// by looking up its Workspace object via orgsClient (scoped to root:orgs).
func getOrgClusterID(ctx context.Context, orgsClient client.Client, orgName string) (string, error) {
	ws := &kcptenancyv1alpha1.Workspace{}
	if err := orgsClient.Get(ctx, types.NamespacedName{Name: orgName}, ws); err != nil {
		return "", fmt.Errorf("get Workspace %q in root:orgs: %w", orgName, err)
	}
	return ws.Spec.Cluster, nil
}

// buildWorkspaceScopedClient constructs a client targeting the given kcp
// workspace path (e.g. "root:orgs:acme") using rootCfg as the base URL.
func buildWorkspaceScopedClient(rootCfg *rest.Config, scheme *runtime.Scheme, workspacePath string) (client.Client, error) {
	cfg := rest.CopyConfig(rootCfg)
	cfg.Host = fmt.Sprintf("%s/clusters/%s", cfg.Host, workspacePath)
	return client.New(cfg, client.Options{Scheme: scheme})
}

// sanitizeResourceName produces a valid lowercase Kubernetes name.
func sanitizeIndexNamePart(value string) string {
	value = strings.ToLower(value)

	var b strings.Builder
	b.Grow(len(value))
	lastWasDash := false

	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastWasDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasDash = false
		default:
			if !lastWasDash {
				b.WriteByte('-')
				lastWasDash = true
			}
		}
	}

	return strings.Trim(b.String(), "-")
}

func buildCanonicalIndexName(prefix, organizationClusterID, resource string) string {
	parts := make([]string, 0, 3)
	parts = append(parts, prefix)

	if p := sanitizeIndexNamePart(organizationClusterID); p != "" {
		parts = append(parts, p)
	}
	if p := sanitizeIndexNamePart(resource); p != "" {
		parts = append(parts, p)
	}

	indexName := strings.Join(parts, "-")
	if len(indexName) > 255 {
		indexName = indexName[:255]
	}
	return strings.Trim(indexName, "-")
}

// may be removed later. Mgr already scoped for provider
func buildClusterIDScopedClient(rootCfg *rest.Config, scheme *runtime.Scheme, clusterID string) (client.Client, error) {
	cfg := rest.CopyConfig(rootCfg)
	cfg.Host = fmt.Sprintf("%s/clusters/%s", cfg.Host, clusterID)
	return client.New(cfg, client.Options{Scheme: scheme})
}

// GetScopedClient creates a client scoped to a specific logical cluster path (e.g. "root:orgs")
func GetScopedClient(cfg *rest.Config, scheme *runtime.Scheme, clusterPath string) (client.Client, error) {
	scopedCfg := rest.CopyConfig(cfg)
	parsed, err := url.Parse(scopedCfg.Host)
	if err != nil {
		return nil, err
	}
	requestPath := logicalcluster.NewPath(clusterPath).RequestPath()
	parts := strings.Split(parsed.Path, "clusters")
	if len(parts) > 0 {
		parsed.Path, err = url.JoinPath(parts[0], requestPath)
	} else {
		parsed.Path, err = url.JoinPath("/", requestPath)
	}
	if err != nil {
		return nil, err
	}
	scopedCfg.Host = parsed.String()
	return client.New(scopedCfg, client.Options{Scheme: scheme})
}
