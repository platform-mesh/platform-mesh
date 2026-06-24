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

	"github.com/rs/zerolog/log"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

type workspaceAuthSubroutine struct {
	runtimeClient   ctrlruntimeclient.Client
	mgr             mcmanager.Manager
	kcpClientGetter iclient.KCPClientGetter
	cfg             config.Config
}

func NewWorkspaceAuthConfigurationSubroutine(runtimeClient ctrlruntimeclient.Client, mgr mcmanager.Manager, kcpClientGetter iclient.KCPClientGetter, cfg config.Config) *workspaceAuthSubroutine {
	return &workspaceAuthSubroutine{
		runtimeClient:   runtimeClient,
		mgr:             mgr,
		kcpClientGetter: kcpClientGetter,
		cfg:             cfg,
	}
}

var (
	_ subroutines.Initializer = &workspaceAuthSubroutine{}
	_ subroutines.Processor   = &workspaceAuthSubroutine{}
)

func (r *workspaceAuthSubroutine) GetName() string { return "workspaceAuthConfiguration" }

// Initialize implements subroutines.Initializer.
func (r *workspaceAuthSubroutine) Initialize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	return r.reconcile(ctx, obj)
}

// Process implements subroutines.Processor.
func (r *workspaceAuthSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	return r.reconcile(ctx, obj)
}

func (r *workspaceAuthSubroutine) reconcile(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	lc := obj.(*kcpcorev1alpha1.LogicalCluster)

	workspaceName := getWorkspaceName(lc)
	if workspaceName == "" {
		return subroutines.OK(), fmt.Errorf("failed to get workspace path")
	}

	var domainCASecret corev1.Secret
	if r.cfg.DomainCALookup {
		err := r.runtimeClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "domain-certificate-ca", Namespace: "platform-mesh-system"}, &domainCASecret)
		if err != nil {
			return subroutines.OK(), fmt.Errorf("failed to get domain CA secret: %w", err)
		}
	}

	cluster, err := r.mgr.ClusterFromContext(ctx)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get cluster from context %w", err)
	}

	var accountInfo pmcorev1alpha1.AccountInfo
	err = cluster.GetClient().Get(ctx, types.NamespacedName{Name: "account"}, &accountInfo)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get AccountInfo: %w", err)
	}

	if accountInfo.Spec.OIDC == nil || len(accountInfo.Spec.OIDC.Clients) == 0 {
		return subroutines.OK(), fmt.Errorf("AccountInfo %s has no OIDC clients", workspaceName)
	}

	audiences := make([]string, 0, len(accountInfo.Spec.OIDC.Clients)+len(r.cfg.AdditionalAudiences))
	for clientName, clientInfo := range accountInfo.Spec.OIDC.Clients {
		if clientInfo.ClientID == "" {
			return subroutines.OK(), fmt.Errorf("OIDC client %s has empty ClientID in AccountInfo", clientName)
		}
		audiences = append(audiences, clientInfo.ClientID)
	}
	audiences = append(audiences, r.cfg.AdditionalAudiences...)

	jwtAuthenticationConfiguration := kcptenancyv1alpha1.JWTAuthenticator{
		Issuer: kcptenancyv1alpha1.Issuer{
			URL:                 fmt.Sprintf("https://%s/keycloak/realms/%s", r.cfg.BaseDomain, workspaceName),
			AudienceMatchPolicy: kcptenancyv1alpha1.AudienceMatchPolicyMatchAny,
			Audiences:           audiences,
		},
		ClaimMappings: kcptenancyv1alpha1.ClaimMappings{
			Groups: kcptenancyv1alpha1.PrefixedClaimOrExpression{
				Claim:  r.cfg.GroupClaim,
				Prefix: ptr.To(""),
			},
			Username: kcptenancyv1alpha1.PrefixedClaimOrExpression{}, // to be set based on environment
		},
	}

	// If production - default behavior - only verified emails.
	if !r.cfg.DevelopmentAllowUnverifiedEmails {
		jwtAuthenticationConfiguration.ClaimMappings.Username = kcptenancyv1alpha1.PrefixedClaimOrExpression{
			Claim:  r.cfg.UserClaim,
			Prefix: ptr.To(""),
		}
	} else {
		// Development mode - allow both verified and unverified emails.
		jwtAuthenticationConfiguration.ClaimMappings.Username = kcptenancyv1alpha1.PrefixedClaimOrExpression{
			Expression: "claims.email",
		}
		jwtAuthenticationConfiguration.ClaimValidationRules = []kcptenancyv1alpha1.ClaimValidationRule{
			{
				Expression: "claims.?email_verified.orValue(true) == true || claims.?email_verified.orValue(true) == false",
				Message:    "Allowing both verified and unverified emails",
			}}

	}

	orgsClient, err := r.kcpClientGetter.NewClientForLogicalCluster(ctx, "root:orgs")
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting orgs client: %w", err)
	}

	authConfig := &kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration{ObjectMeta: metav1.ObjectMeta{Name: workspaceName}}
	_, err = controllerutil.CreateOrUpdate(ctx, orgsClient, authConfig, func() error {
		authConfig.Spec = kcptenancyv1alpha1.WorkspaceAuthenticationConfigurationSpec{
			JWT: []kcptenancyv1alpha1.JWTAuthenticator{
				jwtAuthenticationConfiguration,
			},
		}

		if r.cfg.DomainCALookup {
			authConfig.Spec.JWT[0].Issuer.CertificateAuthority = string(domainCASecret.Data["tls.crt"])
		}

		return nil
	})
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to create WorkspaceAuthConfiguration resource: %w", err)
	}

	err = r.patchWorkspaceTypes(ctx, orgsClient, workspaceName)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to patch workspace types: %w", err)
	}

	return subroutines.OK(), nil
}

func (r *workspaceAuthSubroutine) patchWorkspaceTypes(ctx context.Context, cl ctrlruntimeclient.Client, workspaceName string) error {
	wsTypeList := &kcptenancyv1alpha1.WorkspaceTypeList{}
	if err := cl.List(ctx, wsTypeList, ctrlruntimeclient.MatchingLabels{"core.platform-mesh.io/org": workspaceName}); err != nil {
		return fmt.Errorf("failed to list WorkspaceTypes: %w", err)
	}

	desiredAuthConfig := []kcptenancyv1alpha1.AuthenticationConfigurationReference{
		{Name: workspaceName},
	}

	for _, wsType := range wsTypeList.Items {
		if equality.Semantic.DeepEqual(wsType.Spec.AuthenticationConfigurations, desiredAuthConfig) {
			log.Debug().Msg(fmt.Sprintf("workspaceType %s already has authentication configuration, skip patching", wsType.Name))
			continue
		}

		original := wsType.DeepCopy()
		wsType.Spec.AuthenticationConfigurations = desiredAuthConfig

		if err := cl.Patch(ctx, &wsType, ctrlruntimeclient.MergeFrom(original)); err != nil {
			return fmt.Errorf("failed to patch WorkspaceType %s: %w", wsType.Name, err)
		}
		log.Debug().Msg(fmt.Sprintf("patched workspaceType %s with authentication configuration", wsType.Name))
	}

	return nil
}
