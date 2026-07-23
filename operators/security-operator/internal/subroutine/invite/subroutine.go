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

package invite

import (
	"context"
	"fmt"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/idp"
	"go.platform-mesh.io/subroutines"

	"k8s.io/client-go/util/workqueue"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

type subroutine struct {
	provider        idp.Provider
	baseDomain      string
	kcpClientGetter client.KCPClientGetter
	limiter         workqueue.TypedRateLimiter[*pmcorev1alpha1.Invite]
}

// 2-legged provider please
func New(ctx context.Context, cfg *config.Config, provider idp.Provider, kcpClientGetter client.KCPClientGetter) (*subroutine, error) {
	lim, err := ratelimiter.NewStaticThenExponentialRateLimiter[*pmcorev1alpha1.Invite](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}

	return &subroutine{
		provider:        provider,
		baseDomain:      cfg.BaseDomain,
		kcpClientGetter: kcpClientGetter,
		limiter:         lim,
	}, nil
}

var _ subroutines.Processor = &subroutine{}
var _ subroutines.Subroutine = &subroutine{}

func (s *subroutine) GetName() string { return "Invite" }

func (s *subroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	invite := obj.(*pmcorev1alpha1.Invite)
	log := logger.LoadLoggerFromContext(ctx)

	log.Debug().Str("email", invite.Spec.Email).Msg("Processing invite")

	clusterName, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.OK(), fmt.Errorf("failed to get cluster from context")
	}

	cl, err := s.kcpClientGetter.NewClientForLogicalCluster(ctx, string(clusterName))
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get client for cluster %q: %w", clusterName, err)
	}

	var accountInfo pmcorev1alpha1.AccountInfo
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "account"}, &accountInfo); err != nil {
		log.Err(err).Msg("Failed to get AccountInfo")
		return subroutines.OK(), err
	}

	realm := accountInfo.Spec.Organization.Name
	if realm == "" {
		log.Error().Msg("Organization name is empty in AccountInfo")
		return subroutines.OK(), fmt.Errorf("organization name is empty in AccountInfo")
	}

	idpUser, err := s.provider.GetUserByEmail(ctx, realm, invite.Spec.Email)
	if err != nil {
		log.Err(err).Msg("Failed to query users")
		return subroutines.OK(), err
	}

	if idpUser != nil {
		log.Info().Str("email", invite.Spec.Email).Msg("User already exists, skipping invite")
		s.limiter.Forget(invite)
		return subroutines.OK(), nil
	}

	log.Info().Str("email", invite.Spec.Email).Msg("User does not exist, creating user and sending invite")

	if accountInfo.Spec.OIDC == nil {
		return subroutines.OK(), fmt.Errorf("AccountInfo OIDC is not configured yet")
	}

	oidcClient, ok := accountInfo.Spec.OIDC.Clients[realm]
	if !ok {
		return subroutines.OK(), fmt.Errorf("failed to get oidc client for organization %s", realm)
	}

	client, err := s.provider.GetClientByID(ctx, realm, oidcClient.ClientID)
	if err != nil {
		log.Err(err).Msg("Failed to get client by ID")
		return subroutines.OK(), err
	}

	if client == nil {
		log.Info().Str("clientId", oidcClient.ClientID).Msg("Client does not exist yet, requeuing")
		return subroutines.StopWithRequeue(s.limiter.When(invite), "client does not exist yet"), nil
	}

	log.Debug().Str("clientId", oidcClient.ClientID).Msg("Client verified")

	// Create user
	inviteLink := fmt.Sprintf("https://%s.%s/", realm, s.baseDomain)
	err = s.provider.CreateUser(ctx, realm, oidcClient.ClientID, invite.Spec.Email, inviteLink)
	if err != nil {
		log.Err(err).Msg("Failed to get create user")
		return subroutines.OK(), err
	}

	log.Debug().Str("email", invite.Spec.Email).Msg("User created")

	s.limiter.Forget(invite)
	return subroutines.OK(), nil
}
