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

package contextual

import (
	"context"
	"fmt"
	"strings"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/clustercache"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/retry"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/util"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

const (
	maxRelationLength   = 50
	systemClusterPrefix = "system:cluster:"
	bindVerb            = "bind"
)

type contextualAuthorizer struct {
	clusterKey          string
	fga                 openfgav1.OpenFGAServiceClient
	clusterCache        clustercache.Provider
	cacheMissTracker    retry.Tracker[string]
	cacheMissRetryAfter time.Duration
}

var _ authorization.Handler = &contextualAuthorizer{}

func New(fga openfgav1.OpenFGAServiceClient, clusterCache clustercache.Provider, clusterKey string, cacheMissTracker retry.Tracker[string], cacheMissRetryAfter time.Duration) authorization.Handler {
	return &contextualAuthorizer{
		fga:                 fga,
		clusterKey:          clusterKey,
		clusterCache:        clusterCache,
		cacheMissTracker:    cacheMissTracker,
		cacheMissRetryAfter: cacheMissRetryAfter,
	}
}

func (c *contextualAuthorizer) Handle(ctx context.Context, req authorization.Request) authorization.Response {
	klog.V(5).Info("handling request in ContextualAuthorizer")

	if req.Spec.Extra == nil {
		klog.V(5).Info("request does not contain Extra attributes, skipping")
		return authorization.NoOpinion()
	}

	attrs := req.Spec.ResourceAttributes

	cn, ok := req.Spec.Extra[c.clusterKey]
	if !ok || len(cn) == 0 {
		klog.V(5).Infof("request does not contain expected Extra attribute %q, skipping", c.clusterKey)
		return authorization.NoOpinion()
	}

	clusterName := cn[0]

	klog.V(5).InfoS("found cluster name", "clusterName", clusterName)

	if req.Spec.ResourceAttributes == nil {
		klog.V(5).Info("request does not contain ResourceAttributes, skipping")
		return authorization.NoOpinion()
	}

	// Handle bind verb for kcp separately
	// it requires consumer and provider cluster info
	if attrs.Verb == bindVerb && attrs.Group == "apis.kcp.io" {
		return c.handleKCPBindCheck(ctx, req)
	}

	clusterInfo, ok := c.clusterCache.Get(multicluster.ClusterName(clusterName))
	if !ok {
		if c.cacheMissTracker.ShouldRetry(clusterName) {
			klog.V(5).InfoS("cluster not found in cache, retrying", "clusterName", clusterName)
			c.cacheMissTracker.Retried(clusterName)
			return authorization.Retry(c.cacheMissRetryAfter)
		}

		klog.V(5).InfoS("cluster not found in cache", "clusterName", clusterName)
		return authorization.NoOpinion()
	}

	klog.V(5).InfoS("found cluster info in cache",
		"clusterName", clusterName,
		"storeID", clusterInfo.StoreID,
		"accountName", clusterInfo.AccountName,
		"parentClusterID", clusterInfo.ParentClusterID)

	version := attrs.Version
	if version == "*" {
		// For some cluster level resources, the version may be set to "*". In that case, we should treat it as empty string to avoid issues with RESTMapper.
		version = ""
	}

	gvr := schema.GroupVersionResource{
		Group:    attrs.Group,
		Version:  version,
		Resource: attrs.Resource,
	}

	gvk, err := clusterInfo.RESTMapper.KindFor(gvr)
	if err != nil {
		klog.ErrorS(err, "failed to get GVK for GVR", "GVR", gvr)
		return authorization.NoOpinion()
	}

	klog.V(5).InfoS("mapped GVR to GVK", "GVK", gvk)

	isNamespaced, err := apiutil.IsGVKNamespaced(gvk, clusterInfo.RESTMapper)
	if err != nil {
		klog.ErrorS(err, "failed to determine if GVK is namespaced", "GVK", gvk)
		return authorization.NoOpinion()
	}

	singular, err := clusterInfo.RESTMapper.ResourceSingularizer(attrs.Resource)
	if err != nil {
		klog.ErrorS(err, "failed to singularize resource", "resource", attrs.Resource)
		return authorization.NoOpinion()
	}

	group, objectType := buildObjectType(gvr, singular)

	object := fmt.Sprintf("%s:%s/%s", objectType, clusterName, attrs.Name)
	relation := attrs.Verb

	hasParent := util.ResolveOnParent(attrs.Verb)

	accountObject := fmt.Sprintf("core_platform-mesh_io_account:%s/%s", clusterInfo.ParentClusterID, clusterInfo.AccountName)

	if hasParent {
		relation = fmt.Sprintf("%s_%s_%s", relation, group, gvr.Resource)
		object = accountObject
	}

	var contextualTuples []*openfgav1.TupleKey
	if isNamespaced {
		namespaceObject := fmt.Sprintf("core_namespace:%s/%s", clusterName, attrs.Namespace)

		// parent the namespace to the account
		contextualTuples = append(contextualTuples, &openfgav1.TupleKey{
			Object:   namespaceObject,
			Relation: "parent",
			User:     accountObject,
		})

		if hasParent {
			object = namespaceObject
		} else {
			// parent the object to the namespace
			contextualTuples = append(contextualTuples, &openfgav1.TupleKey{
				Object:   object,
				Relation: "parent",
				User:     namespaceObject,
			})
		}
	} else {
		contextualTuples = append(contextualTuples, &openfgav1.TupleKey{
			Object:   fmt.Sprintf("%s:%s/%s", objectType, clusterName, attrs.Name),
			Relation: "parent",
			User:     accountObject,
		})
	}

	klog.InfoS("calling fga", "object", object, "relation", relation)

	check := &openfgav1.CheckRequest{
		StoreId: clusterInfo.StoreID,
		TupleKey: &openfgav1.CheckRequestTupleKey{
			Object:   object,
			Relation: relation,
			User:     fmt.Sprintf("user:%s", req.Spec.User),
		},
	}

	if contextualTuples != nil {
		check.ContextualTuples = &openfgav1.ContextualTupleKeys{
			TupleKeys: contextualTuples,
		}
	}

	response, err := c.fga.Check(ctx, check)
	if err != nil {
		klog.ErrorS(err, "failed to perform OpenFGA check")
		return authorization.NoOpinion()
	}

	klog.V(5).InfoS("performed OpenFGA check", "allowed", response.Allowed)

	if response.Allowed {
		return authorization.Allowed()
	}

	return authorization.NoOpinion()
}

func (c *contextualAuthorizer) handleKCPBindCheck(ctx context.Context, req authorization.Request) authorization.Response {
	attrs := req.Spec.ResourceAttributes
	if attrs == nil {
		klog.V(5).Info("bind request does not contain ResourceAttributes, skipping")
		return authorization.NoOpinion()
	}

	providerCluster, ok := req.Spec.Extra[c.clusterKey]
	if !ok || len(providerCluster) == 0 {
		klog.V(5).InfoS("bind request missing provider cluster in Extra", "clusterKey", c.clusterKey)
		return authorization.NoOpinion()
	}
	providerClusterName := providerCluster[0]

	consumerClusterID := ""
	for _, group := range req.Spec.Groups {
		if id, ok := strings.CutPrefix(group, systemClusterPrefix); ok {
			consumerClusterID = id
			break
		}
	}

	if consumerClusterID == "" {
		klog.V(5).InfoS("bind request missing consumer cluster in Groups")
		return authorization.NoOpinion()
	}

	consumerInfo, ok := c.clusterCache.Get(multicluster.ClusterName(consumerClusterID))
	if !ok {
		if c.cacheMissTracker.ShouldRetry(consumerClusterID) {
			klog.V(5).InfoS("consumer cluster not found in cache, retrying", "clusterID", consumerClusterID)
			c.cacheMissTracker.Retried(consumerClusterID)
			return authorization.Retry(c.cacheMissRetryAfter)
		}
		klog.V(5).InfoS("consumer cluster not found in cache", "clusterID", consumerClusterID)
		return authorization.NoOpinion()
	}

	klog.V(5).InfoS("fetched consumer cluster info from cache",
		"consumerAccount", consumerInfo.AccountName,
		"consumerParentClusterID", consumerInfo.ParentClusterID,
		"storeID", consumerInfo.StoreID)

	singular, err := consumerInfo.RESTMapper.ResourceSingularizer(attrs.Resource)
	if err != nil {
		klog.ErrorS(err, "failed to singularize resource", "resource", attrs.Resource)
		return authorization.NoOpinion()
	}

	gvr := schema.GroupVersionResource{
		Group:    attrs.Group,
		Resource: attrs.Resource,
	}

	_, resourceObjectType := buildObjectType(gvr, singular)

	resourceToBind := fmt.Sprintf("%s:%s/%s", resourceObjectType, providerClusterName, attrs.Name)
	consumerAccountObject := fmt.Sprintf("core_platform-mesh_io_account:%s/%s",
		consumerInfo.ParentClusterID,
		consumerInfo.AccountName)

	check := &openfgav1.CheckRequest{
		StoreId: consumerInfo.StoreID,
		TupleKey: &openfgav1.CheckRequestTupleKey{
			Object:   consumerAccountObject,
			Relation: bindVerb,
			User:     resourceToBind,
		},
	}
	klog.InfoS("calling fga", "object", consumerAccountObject, "relation", attrs.Verb)

	response, err := c.fga.Check(ctx, check)
	if err != nil {
		klog.ErrorS(err, "failed to perform OpenFGA check for bind")
		return authorization.NoOpinion()
	}

	klog.InfoS("performed OpenFGA bind check", "allowed", response.Allowed)

	if response.Allowed {
		return authorization.Allowed()
	}

	return authorization.NoOpinion()
}

func buildObjectType(gvr schema.GroupVersionResource, singular string) (string, string) {
	group := util.CapGroupToRelationLength(gvr, maxRelationLength)
	group = strings.ReplaceAll(group, ".", "_")

	objectType := fmt.Sprintf("%s_%s", group, singular)
	longestObjectType := fmt.Sprintf("create_%ss", objectType)
	if len(longestObjectType) > maxRelationLength {
		objectType = objectType[len(longestObjectType)-maxRelationLength:]
	}

	return group, objectType
}
