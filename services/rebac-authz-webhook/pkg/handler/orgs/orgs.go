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

package orgs

import (
	"context"
	"fmt"
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/util"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcpcorev1alpha "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

const rootOrgName = "tenancy_kcp_io_workspace:orgs"

type orgsAuthorizer struct {
	clusterKey  string
	orgsStoreID string
	fga         openfgav1.OpenFGAServiceClient
	mgr         mcmanager.Manager
}

var _ authorization.Handler = &orgsAuthorizer{}

func New(fga openfgav1.OpenFGAServiceClient, mgr mcmanager.Manager, clusterKey, orgsStoreID string) authorization.Handler {
	return &orgsAuthorizer{
		clusterKey:  clusterKey,
		orgsStoreID: orgsStoreID,
		fga:         fga,
		mgr:         mgr,
	}
}

func (o *orgsAuthorizer) Handle(ctx context.Context, req authorization.Request) authorization.Response {

	klog.V(5).Info("handling request in OrgsAuthorizer")

	if req.Spec.Extra == nil {
		klog.V(5).Info("request does not contain Extra attributes, skipping")
		return authorization.NoOpinion()
	}

	cn, ok := req.Spec.Extra[o.clusterKey]
	if !ok || len(cn) == 0 {
		klog.V(5).Infof("request does not contain expected Extra attribute %q, skipping", o.clusterKey)
		return authorization.NoOpinion()
	}

	clusterName := cn[0]

	if req.Spec.ResourceAttributes == nil {
		klog.V(5).Info("request does not contain ResourceAttributes, skipping")
		return authorization.NoOpinion()
	}

	orgsWorkspaceID, err := o.getOrgsWorkspaceID(ctx)
	if err != nil {
		klog.ErrorS(err, "failed to retrieve orgs workspace ID")
		return authorization.NoOpinion()
	}

	if clusterName != orgsWorkspaceID {
		klog.V(5).Infof("request cluster name %q does not match org workspace ID %q, skipping", clusterName, orgsWorkspaceID)
		return authorization.NoOpinion()
	}
	klog.V(2).Infof("request cluster name %q matches org workspace ID %q, requesting fga", clusterName, orgsWorkspaceID)

	attrs := req.Spec.ResourceAttributes

	group := util.CapGroupToRelationLength(schema.GroupVersionResource{Group: attrs.Group, Version: attrs.Version, Resource: attrs.Resource}, 50)
	group = strings.ReplaceAll(group, ".", "_")

	res, err := o.fga.Check(ctx, &openfgav1.CheckRequest{
		StoreId: o.orgsStoreID,
		TupleKey: &openfgav1.CheckRequestTupleKey{
			Object:   rootOrgName,
			Relation: fmt.Sprintf("%s_%s_%s", attrs.Verb, group, attrs.Resource),
			User:     fmt.Sprintf("user:%s", req.Spec.User),
		},
	})
	if err != nil {
		klog.Errorf("error checking fga for user %q in orgs store %q: %v", req.Spec.User, o.orgsStoreID, err)
		return authorization.NoOpinion()
	}

	if res.Allowed {
		return authorization.Allowed()
	}

	return authorization.Aborted()
}

func (o *orgsAuthorizer) getOrgsWorkspaceID(ctx context.Context) (string, error) {
	orgsCluster, err := o.mgr.GetCluster(ctx, "root:orgs")
	if err != nil {
		return "", err
	}

	orgsLC := kcpcorev1alpha.LogicalCluster{}
	err = orgsCluster.GetClient().Get(ctx, types.NamespacedName{Name: "cluster"}, &orgsLC)
	if err != nil {
		return "", err
	}

	orgsWorkspaceID, ok := orgsLC.Annotations["kcp.io/cluster"]
	if !ok {
		return "", fmt.Errorf("kcp.io/cluster annotation not found")
	}

	return orgsWorkspaceID, nil
}
