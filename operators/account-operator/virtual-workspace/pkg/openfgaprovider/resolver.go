/*
Copyright 2026 The kcp Authors.

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

// Package openfgaprovider answers SelfClusterAccessReviews from OpenFGA
// instead of Kubernetes RBAC.
//
// It asks OpenFGA at review time and keeps no copy of the relations: OpenFGA
// resolves inheritance and wildcard users itself, and the answer is never
// older than the store.
//
// The provider follows the conventions of Platform Mesh, which keeps
//
//   - one OpenFGA store per organization. An organization is a workspace
//     directly below the organizations workspace (root:orgs), and its store id
//     is in the status of the Store object of the same name there;
//   - an account as the object <type>:<parent cluster>/<name>, where the
//     account's own workspace is the Workspace <name> in <parent cluster>.
//
// A review therefore has to be scoped to a workspace of one organization,
// /services/access/clusters/<workspace>/..., and is answered with one
// ListObjects call against that organization's store.
package openfgaprovider

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"google.golang.org/grpc"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/accessprovider"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/graph"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"github.com/kcp-dev/logicalcluster/v3"
)

const (
	// DefaultRelation is what "can access" means: the relation a user needs on
	// an account for its workspace to be listed.
	DefaultRelation = "get"
	// DefaultObjectType is the OpenFGA type of a Platform Mesh account.
	DefaultObjectType = "core_platform-mesh_io_account"
	// DefaultOrgsWorkspace is the workspace holding the organizations.
	DefaultOrgsWorkspace = "root:orgs"

	// lookupConcurrency bounds the workspace lookups of one review.
	lookupConcurrency = 8
)

// ObjectLister is the part of the OpenFGA API the resolver uses.
// openfgav1.OpenFGAServiceClient implements it.
type ObjectLister interface {
	ListObjects(ctx context.Context, in *openfgav1.ListObjectsRequest, opts ...grpc.CallOption) (*openfgav1.ListObjectsResponse, error)
}

// Directory resolves what the resolver needs to know about kcp.
type Directory interface {
	// WorkspacePath returns the workspace path of a logical cluster name.
	WorkspacePath(ctx context.Context, cluster string) (path logicalcluster.Path, found bool, err error)
	// StoreID returns the id of the OpenFGA store of an organization.
	StoreID(ctx context.Context, org string) (id string, found bool, err error)
	// WorkspaceCluster returns the logical cluster of the workspace name in
	// the logical cluster parent.
	WorkspaceCluster(ctx context.Context, parent, name string) (cluster string, found bool, err error)
	// AccountDetails returns what a list of accounts shows of the Account
	// name in the logical cluster parent.
	AccountDetails(ctx context.Context, parent, name string) (details AccountDetails, found bool, err error)
}

// AccountDetails is the part of an Account a review carries.
type AccountDetails struct {
	// DisplayName is spec.displayName.
	DisplayName string
	// Type is spec.type: org or account.
	Type string
	// Ready is whether the condition Ready has the status True.
	Ready bool
}

// Options configure a Resolver. Empty fields take the defaults.
type Options struct {
	// Relation a user needs on an account for it to be listed.
	Relation string
	// ObjectType is the OpenFGA type of an account.
	ObjectType string
	// OrgsWorkspace is the path of the workspace holding the organizations.
	OrgsWorkspace string
	// EndpointBase is the front-proxy URL prefix; a cluster's endpoint is
	// EndpointBase + <cluster name>.
	EndpointBase string
}

// Resolver answers reviews from OpenFGA.
type Resolver struct {
	fga       ObjectLister
	directory Directory
	opts      Options
}

var _ accessprovider.AccessProvider = &Resolver{}

// New returns a Resolver asking fga, and directory for what it needs from kcp.
func New(fga ObjectLister, directory Directory, opts Options) *Resolver {
	if opts.Relation == "" {
		opts.Relation = DefaultRelation
	}
	if opts.ObjectType == "" {
		opts.ObjectType = DefaultObjectType
	}
	if opts.OrgsWorkspace == "" {
		opts.OrgsWorkspace = DefaultOrgsWorkspace
	}
	if opts.EndpointBase != "" && !strings.HasSuffix(opts.EndpointBase, "/") {
		opts.EndpointBase += "/"
	}

	return &Resolver{fga: fga, directory: directory, opts: opts}
}

// Ready is always true: there is no state to build before the first review.
func (r *Resolver) Ready() bool {
	return true
}

// ClustersFor returns the workspaces of the accounts the user can access in
// the organization the cluster belongs to. Only the name of the identity
// plays a role: the OpenFGA model relates users, not groups, and nothing in
// it is conditional on the extra of an identity.
//
// It never returns a partial answer: if OpenFGA or kcp cannot be asked, the
// review fails with ServiceUnavailable.
func (r *Resolver) ClustersFor(ctx context.Context, id accessprovider.Identity, cluster string) ([]graph.AccessEndpointSlice, error) {
	if cluster == "" || cluster == "*" {
		return nil, apierrors.NewBadRequest(
			"access is resolved per organization: address the review to a workspace of the organization, " +
				"/services/access/clusters/<workspace>/apis/access.platform-mesh.io/v1alpha1/selfclusteraccessreviews")
	}

	logger := klog.FromContext(ctx).WithValues("scope", cluster, "username", id.Name)

	org, found, err := r.organization(ctx, cluster)
	if err != nil {
		logger.Error(err, "resolving the organization of the scope")
		return nil, apierrors.NewServiceUnavailable("the organization of the workspace cannot be resolved; try again shortly")
	}
	if !found {
		logger.V(4).Info("scope is not inside an organization, nothing to list")
		return nil, nil
	}

	storeID, found, err := r.directory.StoreID(ctx, org)
	if err != nil {
		logger.Error(err, "looking up the OpenFGA store", "organization", org)
		return nil, apierrors.NewServiceUnavailable("the authorization store of the organization cannot be resolved; try again shortly")
	}
	if !found {
		logger.V(4).Info("organization has no OpenFGA store, nothing to list", "organization", org)
		return nil, nil
	}

	resp, err := r.fga.ListObjects(ctx, &openfgav1.ListObjectsRequest{
		StoreId:  storeID,
		Type:     r.opts.ObjectType,
		Relation: r.opts.Relation,
		User:     fgaUser(id.Name),
	})
	if err != nil {
		logger.Error(err, "listing objects in OpenFGA", "organization", org, "store", storeID)
		return nil, apierrors.NewServiceUnavailable("the authorization service cannot be asked; try again shortly")
	}

	clusters, err := r.clustersOf(ctx, resp.GetObjects())
	if err != nil {
		logger.Error(err, "mapping accounts to workspaces", "organization", org)
		return nil, apierrors.NewServiceUnavailable("the workspaces of the accounts cannot be resolved; try again shortly")
	}

	out := make([]graph.AccessEndpointSlice, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, graph.AccessEndpointSlice{ClusterName: c.cluster, Endpoint: r.opts.EndpointBase + c.cluster, Account: c.account})
	}

	logger.V(4).Info("resolved access from OpenFGA", "organization", org, "accounts", len(resp.GetObjects()), "clusters", len(out))

	return out, nil
}

// organization returns the name of the organization a workspace belongs to:
// the workspace below the organizations workspace on its path.
func (r *Resolver) organization(ctx context.Context, cluster string) (string, bool, error) {
	path := logicalcluster.NewPath(cluster)

	// A logical cluster name has a single segment; so has the path "root",
	// which is never inside an organization either way.
	if !strings.Contains(cluster, ":") {
		if cluster == "root" {
			return "", false, nil
		}

		resolved, found, err := r.directory.WorkspacePath(ctx, cluster)
		if err != nil || !found {
			return "", false, err
		}
		path = resolved
	}

	below, inside := strings.CutPrefix(path.String(), r.opts.OrgsWorkspace+":")
	if !inside {
		return "", false, nil
	}
	org, _, _ := strings.Cut(below, ":")

	return org, org != "", nil
}

// accountCluster is the logical cluster of an account's workspace.
type accountCluster struct {
	cluster string
	account *graph.Account
}

// clustersOf maps account objects to the logical clusters of their
// workspaces, sorted by cluster and without duplicates, each with the account
// it belongs to. An account whose workspace does not exist (yet, or any more)
// is left out.
func (r *Resolver) clustersOf(ctx context.Context, objects []string) ([]accountCluster, error) {
	type result struct {
		accountCluster
		err error
	}

	results := make([]result, len(objects))
	limit := make(chan struct{}, lookupConcurrency)
	var wg sync.WaitGroup

	for i, object := range objects {
		parent, name, ok := parseAccount(r.opts.ObjectType, object)
		if !ok {
			klog.FromContext(ctx).Info("skipping OpenFGA object that is not an account", "object", object)
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()

			cluster, found, err := r.directory.WorkspaceCluster(ctx, parent, name)
			if err != nil {
				results[i].err = fmt.Errorf("workspace %s in %s: %w", name, parent, err)
				return
			}
			if !found {
				klog.FromContext(ctx).V(2).Info("skipping account without a workspace", "object", object)
				return
			}

			account, err := r.account(ctx, parent, name)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].accountCluster = accountCluster{cluster: cluster, account: account}
		}()
	}
	wg.Wait()

	seen := make(map[string]struct{}, len(results))
	clusters := make([]accountCluster, 0, len(results))
	for _, res := range results {
		if res.err != nil {
			return nil, res.err
		}
		if res.cluster == "" {
			continue
		}
		if _, dup := seen[res.cluster]; dup {
			continue
		}
		seen[res.cluster] = struct{}{}
		clusters = append(clusters, res.accountCluster)
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].cluster < clusters[j].cluster })

	return clusters, nil
}

// account describes the account name in the logical cluster parent. The path
// and the details are left empty where kcp does not have them (yet); the
// account is listed by its name then.
func (r *Resolver) account(ctx context.Context, parent, name string) (*graph.Account, error) {
	account := &graph.Account{Name: name}

	parentPath, found, err := r.directory.WorkspacePath(ctx, parent)
	if err != nil {
		return nil, fmt.Errorf("path of %s: %w", parent, err)
	}
	if found {
		account.Path = parentPath.Join(name).String()
	}

	details, found, err := r.directory.AccountDetails(ctx, parent, name)
	if err != nil {
		return nil, fmt.Errorf("account %s in %s: %w", name, parent, err)
	}
	if found {
		account.DisplayName = details.DisplayName
		account.Type = details.Type
		account.Ready = details.Ready
	}

	return account, nil
}

// fgaUser renders a kcp username the way Platform Mesh writes it into
// tuples: OpenFGA allows one colon in a user, so colons become dots.
func fgaUser(name string) string {
	return "user:" + strings.ReplaceAll(name, ":", ".")
}

// parseAccount splits the object <objectType>:<parent cluster>/<name>.
func parseAccount(objectType, object string) (parent, name string, ok bool) {
	id, ok := strings.CutPrefix(object, objectType+":")
	if !ok {
		return "", "", false
	}

	parent, name, ok = strings.Cut(id, "/")
	if !ok || parent == "" || name == "" {
		return "", "", false
	}

	// Names are percent-encoded where they contain what OpenFGA forbids.
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}

	return parent, name, true
}
