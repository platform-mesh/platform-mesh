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
	"net/http"
	"net/url"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/gqlerror"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/iam-service/pkg/accountinfo"
	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	"go.platform-mesh.io/iam-service/pkg/graph"
	"go.platform-mesh.io/iam-service/pkg/metrics"
	"go.platform-mesh.io/iam-service/pkg/workspace"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	authorizationv1client "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// selfSubjectAccessReviewer issues a SelfSubjectAccessReview against a kcp
// workspace using the end user's identity. It is abstracted so tests can inject
// a fake without a live apiserver.
type selfSubjectAccessReviewer func(ctx context.Context, workspacePath, authHeader string, attrs *authorizationv1.ResourceAttributes) (bool, error)

type AuthorizedDirective struct {
	air      accountinfo.Retriever
	wcClient workspace.ClientFactory
	ssar     selfSubjectAccessReviewer
	log      *logger.Logger
}

// NewAuthorizedDirective creates the directive that authorizes GraphQL fields by
// issuing a native SelfSubjectAccessReview (SSAR) against the user's workspace.
// The check flows kcp -> rebac-authz-webhook -> OpenFGA, so OpenFGA stays an
// implementation detail rather than a direct dependency of iam-service.
func NewAuthorizedDirective(air accountinfo.Retriever, cf workspace.ClientFactory, restCfg *rest.Config, log *logger.Logger) *AuthorizedDirective {
	d := &AuthorizedDirective{
		air:      air,
		wcClient: cf,
		log:      log,
	}
	d.ssar = newRESTSelfSubjectAccessReviewer(restCfg)
	return d
}

// NewAuthorizedDirectiveWithReviewer creates a directive with a custom SSAR
// implementation. Intended for testing with a fake reviewer.
func NewAuthorizedDirectiveWithReviewer(air accountinfo.Retriever, cf workspace.ClientFactory, ssar selfSubjectAccessReviewer, log *logger.Logger) *AuthorizedDirective {
	return &AuthorizedDirective{
		air:      air,
		wcClient: cf,
		ssar:     ssar,
		log:      log,
	}
}

func (a AuthorizedDirective) Authorized(ctx context.Context, _ any, next graphql.Resolver, permission string) (any, error) {
	a.log.Debug().Msg("Authorized directive called with permission: " + permission)

	if _, err := pmcontext.GetWebTokenFromContext(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to get web token from context")
	}

	authHeader, err := pmcontext.GetAuthHeaderFromContext(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get auth header from context")
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

	// Authorize via a native SelfSubjectAccessReview against the account's own
	// workspace. kcp routes this through the rebac-authz-webhook, which derives
	// the OpenFGA account object from that workspace's LogicalCluster, so no
	// special-casing of the Account resource's cluster is required here.
	allowed, err := a.testIfAllowed(ctx, rctx, permission, path, authHeader, wsClient)
	if err != nil {
		return nil, errors.Wrap(err, "failed to test if action is allowed")
	}
	if !allowed {
		return nil, gqlerror.Errorf("unauthorized")
	}

	return next(ctx)
}

// testIfAllowed issues a SelfSubjectAccessReview to the given workspace using the
// caller's identity (authHeader), with the permission carried verbatim as the
// SAR verb. The webhook resolves the same OpenFGA relation as the permission
// string, matching the previous direct-Check semantics.
func (a AuthorizedDirective) testIfAllowed(ctx context.Context, rctx *graph.ResourceContext, permission, workspacePath, authHeader string, wsClient ctrlruntimeclient.Client) (bool, error) {
	start := time.Now()
	defer func() {
		metrics.AuthorizationDuration.WithLabelValues(permission).Observe(time.Since(start).Seconds())
	}()

	attrs, err := resourceAttributes(rctx, permission, wsClient)
	if err != nil {
		metrics.AuthorizationChecks.WithLabelValues("error").Inc()
		return false, err
	}

	allowed, err := a.ssar(ctx, workspacePath, authHeader, attrs)
	if err != nil {
		metrics.AuthorizationChecks.WithLabelValues("error").Inc()
		return false, errors.Wrap(err, "failed to perform self subject access review")
	}

	if allowed {
		metrics.AuthorizationChecks.WithLabelValues("allowed").Inc()
	} else {
		metrics.AuthorizationChecks.WithLabelValues("denied").Inc()
	}

	return allowed, nil
}

// resourceAttributes maps a GraphQL ResourceContext to SAR ResourceAttributes.
// The resource's plural name and version are resolved from the workspace's
// RESTMapper (Kind -> GVR). Non-KRM targets (no RESTMapping) are rejected so the
// review fails closed rather than sending ambiguous attributes to the apiserver.
func resourceAttributes(rctx *graph.ResourceContext, permission string, wsClient ctrlruntimeclient.Client) (*authorizationv1.ResourceAttributes, error) {
	mapping, err := wsClient.RESTMapper().RESTMapping(schema.GroupKind{Group: rctx.Group, Kind: rctx.Kind})
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve REST mapping for %s/%s", rctx.Group, rctx.Kind)
	}

	attrs := &authorizationv1.ResourceAttributes{
		Verb:     permission,
		Group:    mapping.Resource.Group,
		Version:  mapping.Resource.Version,
		Resource: mapping.Resource.Resource,
		Name:     rctx.Resource.Name,
	}
	if rctx.Resource.Namespace != nil {
		attrs.Namespace = *rctx.Resource.Namespace
	}

	return attrs, nil
}

// newRESTSelfSubjectAccessReviewer returns a reviewer that POSTs a
// SelfSubjectAccessReview to the given kcp workspace using the caller's
// Authorization header. The apiserver fills in the user identity (and the
// cluster-name Extra the webhook relies on); iam-service never impersonates.
func newRESTSelfSubjectAccessReviewer(baseCfg *rest.Config) selfSubjectAccessReviewer {
	return func(ctx context.Context, workspacePath, authHeader string, attrs *authorizationv1.ResourceAttributes) (bool, error) {
		cfg, err := workspaceConfig(baseCfg, workspacePath, authHeader)
		if err != nil {
			return false, err
		}

		client, err := authorizationv1client.NewForConfig(cfg)
		if err != nil {
			return false, errors.Wrap(err, "failed to create authorization client")
		}

		review := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: attrs,
			},
		}

		res, err := client.SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return res.Status.Allowed, nil
	}
}

// workspaceConfig derives a per-request rest.Config pointing at the kcp
// workspace path and carrying the caller's Authorization header verbatim. Any
// service-account credentials from the base config are stripped so the request
// is authenticated solely as the end user.
func workspaceConfig(baseCfg *rest.Config, workspacePath, authHeader string) (*rest.Config, error) {
	if baseCfg == nil {
		return nil, errors.New("nil rest config")
	}

	cfg := rest.CopyConfig(baseCfg)
	cfg.BearerToken = ""
	cfg.BearerTokenFile = ""
	cfg.CertData = nil
	cfg.CertFile = ""
	cfg.KeyData = nil
	cfg.KeyFile = ""
	cfg.Username = ""
	cfg.Password = ""

	host, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse host from rest config")
	}
	cfg.Host = fmt.Sprintf("%s://%s/clusters/%s", host.Scheme, host.Host, workspacePath)

	cfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return &authHeaderRoundTripper{header: authHeader, next: rt}
	})

	return cfg, nil
}

// authHeaderRoundTripper injects the caller's Authorization header on every
// request so the workspace apiserver authenticates as the end user.
type authHeaderRoundTripper struct {
	header string
	next   http.RoundTripper
}

func (t *authHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", t.header)
	return t.next.RoundTrip(req)
}

func (a AuthorizedDirective) testIfResourceExists(ctx context.Context, rctx *graph.ResourceContext, wsClient ctrlruntimeclient.Client) (bool, error) {
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
	clObj := ctrlruntimeclient.ObjectKey{Name: rctx.Resource.Name}
	if rctx.Resource.Namespace != nil {
		clObj.Namespace = *rctx.Resource.Namespace
	}
	err = wsClient.Get(ctx, clObj, resource)
	if err != nil {
		if ctrlruntimeclient.IgnoreNotFound(err) == nil {
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
