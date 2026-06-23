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

package directive

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/99designs/gqlgen/graphql"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/vektah/gqlparser/v2/gqlerror"
	accountsv1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/fga/util"
	"go.platform-mesh.io/golang-commons/jwt"
	"go.platform-mesh.io/golang-commons/logger"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"go.platform-mesh.io/iam-service/pkg/accountinfo"
	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	"go.platform-mesh.io/iam-service/pkg/fga/store"
	"go.platform-mesh.io/iam-service/pkg/fga/tuples"
	"go.platform-mesh.io/iam-service/pkg/graph"
	"go.platform-mesh.io/iam-service/pkg/metrics"
	"go.platform-mesh.io/iam-service/pkg/workspace"
)

type AuthorizedDirective struct {
	fga      openfgav1.OpenFGAServiceClient
	helper   store.StoreHelper
	air      accountinfo.Retriever
	wcClient workspace.ClientFactory
	log      *logger.Logger
}

func NewAuthorizedDirective(oc openfgav1.OpenFGAServiceClient, air accountinfo.Retriever, storeTTL time.Duration, cf workspace.ClientFactory, log *logger.Logger) *AuthorizedDirective {
	return &AuthorizedDirective{
		fga:      oc,
		helper:   store.NewFGAStoreHelper(storeTTL),
		air:      air,
		wcClient: cf,
		log:      log,
	}
}

// NewAuthorizedDirectiveWithFactory creates a new AuthorizedDirective with a custom ClientFactory.
// This constructor is primarily intended for testing with mock implementations.
func NewAuthorizedDirectiveWithFactory(oc openfgav1.OpenFGAServiceClient, air accountinfo.Retriever, storeTTL time.Duration, clientFactory workspace.ClientFactory) *AuthorizedDirective {
	return &AuthorizedDirective{
		fga:      oc,
		helper:   store.NewFGAStoreHelper(storeTTL),
		air:      air,
		wcClient: clientFactory,
	}
}

func (a AuthorizedDirective) Authorized(ctx context.Context, _ any, next graphql.Resolver, permission string) (any, error) {
	a.log.Debug().Msg("Authorized directive called with permission: " + permission)

	token, err := pmcontext.GetWebTokenFromContext(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get web token from context")
	}

	kctx, err := appcontext.GetKCPContext(ctx)
	if err != nil { // coverage-ignore
		return nil, errors.Wrap(err, "failed to get kcp user context")
	}
	a.log.Debug().Str("context", fmt.Sprintf("%+v", kctx)).Msg("Retrieved kcp context")

	fieldCtx := graphql.GetFieldContext(ctx)
	rctx, err := extractResourceContextFromArguments(fieldCtx.Args)
	if err != nil { // coverage-ignore
		return nil, err
	}
	if rctx == nil {
		return nil, gqlerror.Errorf("resource context is nil")
	}
	a.log.Debug().
		Str("group", rctx.Group).
		Str("kind", rctx.Kind).
		Str("Resource", fmt.Sprintf("%+v", rctx.Resource)).
		Msg("Retrieved resource context")

	// Retrieve account info from kcp workspace
	path := rctx.AccountPath
	if rctx.Group == "core.platform-mesh.io" && rctx.Kind == "Account" {
		path = fmt.Sprintf("%s:%s", path, rctx.Resource.Name)
	}
	ai, err := a.air.Get(ctx, multicluster.ClusterName(path))
	if err != nil { // coverage-ignore
		return nil, errors.Wrap(err, "failed to get account info from kcp context")
	}

	if ai.Spec.Organization.Name != kctx.OrganizationName {
		return nil, gqlerror.Errorf("unauthorized")
	}

	// The clusterID will be set to the cluster where the resource is located.
	// The used account info is from with the account's workspace,
	// so if we manage access for accounts the origin cluster ID must be used.
	clusterId := ai.Spec.Account.GeneratedClusterId
	if rctx.Group == "core.platform-mesh.io" && rctx.Kind == "Account" {
		clusterId = ai.Spec.Account.OriginClusterId
	}
	ctx = appcontext.SetClusterId(ctx, clusterId)

	// Test if resource exists
	wsClient, err := a.wcClient.New(ctx, multicluster.ClusterName(rctx.AccountPath))
	if err != nil { // coverage-ignore
		return nil, errors.Wrap(err, "failed to get workspace client")
	}
	exists, err := a.testIfResourceExists(ctx, rctx, wsClient)
	if err != nil {
		return nil, errors.Wrap(err, "failed to test if resource exists")
	}
	if !exists {
		return nil, gqlerror.Errorf("resource does not exist")
	}

	allowed, err := a.testIfAllowed(ctx, ai, rctx, permission, token)
	if err != nil {
		return nil, errors.Wrap(err, "failed to test if action is allowed")
	}
	if !allowed {
		return nil, gqlerror.Errorf("unauthorized")
	}

	return next(ctx)
}

func (a AuthorizedDirective) testIfAllowed(ctx context.Context, ai *accountsv1alpha1.AccountInfo, rctx *graph.ResourceContext, permission string, token jwt.WebToken) (bool, error) {
	start := time.Now()
	defer func() {
		metrics.AuthorizationDuration.WithLabelValues(permission).Observe(time.Since(start).Seconds())
	}()

	ct := tuples.GenerateContextualTuples(rctx, ai)

	fgaTypeName := util.ConvertToTypeName(rctx.Group, rctx.Kind)

	clusterId := ai.Spec.Account.GeneratedClusterId
	if rctx.Group == "core.platform-mesh.io" && rctx.Kind == "Account" {
		clusterId = ai.Spec.Account.OriginClusterId
	}

	object := fmt.Sprintf("%s:%s/%s", fgaTypeName, clusterId, rctx.Resource.Name)
	if rctx.Resource.Namespace != nil {
		object = fmt.Sprintf("%s:%s/%s/%s", fgaTypeName, clusterId, *rctx.Resource.Namespace, rctx.Resource.Name)
	}

	user := fmt.Sprintf("user:%s", token.Mail) // TODO: what happens if mail is not uid?
	storeID, err := a.helper.GetStoreID(ctx, a.fga, ai.Spec.Organization.Name)
	if err != nil {
		return false, errors.Wrap(err, "failed to get store ID for organization %s", ai.Spec.Organization.Name)
	}

	req := openfgav1.CheckRequest{
		ContextualTuples: ct,
		StoreId:          storeID,
		TupleKey: &openfgav1.CheckRequestTupleKey{
			Object:   object,
			Relation: permission,
			User:     user,
		},
	}

	res, err := a.fga.Check(ctx, &req)
	if err != nil {
		metrics.AuthorizationChecks.WithLabelValues("error").Inc()
		return false, errors.Wrap(err, "failed to check permission with openfga")
	}

	if res.Allowed {
		metrics.AuthorizationChecks.WithLabelValues("allowed").Inc()
	} else {
		metrics.AuthorizationChecks.WithLabelValues("denied").Inc()
	}

	return res.Allowed, nil
}

func (a AuthorizedDirective) testIfResourceExists(ctx context.Context, rctx *graph.ResourceContext, wsClient client.Client) (bool, error) {
	gvr := schema.GroupVersionResource{
		Group:    rctx.Group,
		Resource: rctx.Kind,
	}

	gvr, err := wsClient.RESTMapper().ResourceFor(gvr)
	if err != nil {
		return false, errors.Wrap(err, "failed to get GVR for resource")
	}

	gvk, err := wsClient.RESTMapper().KindFor(gvr)
	if err != nil { // coverage-ignore
		return false, errors.Wrap(err, "failed to get GVK for resource")
	}

	resource := &unstructured.Unstructured{}
	resource.SetGroupVersionKind(gvk)

	// Try to get the resource
	clObj := client.ObjectKey{Name: rctx.Resource.Name}
	if rctx.Resource.Namespace != nil {
		clObj.Namespace = *rctx.Resource.Namespace
	}
	err = wsClient.Get(ctx, clObj, resource)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return false, nil
		}
	}
	return true, nil
}

const resourceContextParamName = "context"

func extractResourceContextFromArguments(args map[string]any) (*graph.ResourceContext, error) {
	o, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}

	var normalizedArgs map[string]any
	err = json.Unmarshal(o, &normalizedArgs)
	if err != nil { // coverage-ignore
		return nil, err
	}
	val, ok := normalizedArgs[resourceContextParamName]
	if !ok {
		return nil, fmt.Errorf("unable to extract param from request for given paramName %q", resourceContextParamName)
	}
	valBytes, err := json.Marshal(val)
	if err != nil { // coverage-ignore
		return nil, fmt.Errorf("failed to marshal param value: %w", err)
	}
	var paramValue graph.ResourceContext
	err = json.Unmarshal(valBytes, &paramValue)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal param to ResourceContext: %w", err)
	}
	return &paramValue, nil
}
