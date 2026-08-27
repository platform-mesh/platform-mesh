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
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2/clientcredentials"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/subroutines"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func NewInviteSubroutine(ctx context.Context, mgr mcmanager.Manager, kcpClientGetter iclient.KCPClientGetter, cfg config.Config) (*inviteSubroutine, error) {
	lim, err := ratelimiter.NewStaticThenExponentialRateLimiter[*kcpcorev1alpha1.LogicalCluster](
		ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}

	sub := &inviteSubroutine{
		mgr:             mgr,
		kcpClientGetter: kcpClientGetter,
		limiter:         lim,
		userClaim:       cfg.UserClaim,
	}

	if cfg.UserClaim != "email" {
		issuer := fmt.Sprintf("%s/realms/master", cfg.Keycloak.BaseURL)
		provider, err := oidc.NewProvider(ctx, issuer)
		if err != nil {
			return nil, fmt.Errorf("creating OIDC provider for invite email resolution: %w", err)
		}

		cCfg := clientcredentials.Config{
			ClientID:     cfg.Keycloak.ClientID,
			ClientSecret: cfg.Keycloak.ClientSecret,
			TokenURL:     provider.Endpoint().TokenURL,
		}

		sub.keycloak = cCfg.Client(ctx)
		sub.keycloakBaseURL = cfg.Keycloak.BaseURL
		sub.creatorRealm = cfg.CreatorRealm
	}

	return sub, nil
}

var (
	_ subroutines.Initializer = &inviteSubroutine{}
	_ subroutines.Processor   = &inviteSubroutine{}
)

type inviteSubroutine struct {
	mgr             mcmanager.Manager
	kcpClientGetter iclient.KCPClientGetter
	limiter         workqueue.TypedRateLimiter[*kcpcorev1alpha1.LogicalCluster]
	userClaim       string
	keycloak        *http.Client
	keycloakBaseURL string
	creatorRealm    string
}

func (w *inviteSubroutine) GetName() string { return "InviteInitializationSubroutine" }

// Initialize implements subroutines.Initializer.
func (w *inviteSubroutine) Initialize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	return w.reconcile(ctx, obj)
}

// Process implements subroutines.Processor.
func (w *inviteSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	return w.reconcile(ctx, obj)
}

func (w *inviteSubroutine) reconcile(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	lc := obj.(*kcpcorev1alpha1.LogicalCluster)

	wsName := getWorkspaceName(lc)
	if wsName == "" {
		return subroutines.OK(), fmt.Errorf("failed to get workspace name")
	}

	client, err := w.kcpClientGetter.NewClientFromContext(ctx)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting client: %w", err)
	}

	orgsClient, err := w.kcpClientGetter.NewClientForLogicalCluster(ctx, "root:orgs")
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting orgs client: %w", err)
	}
	var account pmcorev1alpha1.Account
	err = orgsClient.Get(ctx, types.NamespacedName{Name: wsName}, &account)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get account resource %w", err)
	}

	if account.Spec.Type != pmcorev1alpha1.AccountTypeOrg {
		log.Info().Str("workspace", wsName).Msg("account is not of type organization, skipping invite creation")
		return subroutines.OK(), nil
	}

	if account.Spec.Creator == nil {
		log.Info().Str("workspace", wsName).Msg("account creator is nil, skipping invite creation")
		return subroutines.OK(), nil
	}

	creatorEmail := *account.Spec.Creator
	if w.userClaim != "email" {
		resolved, err := w.resolveCreatorEmail(ctx, *account.Spec.Creator)
		if err != nil {
			return subroutines.OK(), fmt.Errorf("resolving creator email from user ID %q: %w", *account.Spec.Creator, err)
		}
		creatorEmail = resolved
	}

	// the Invite resource is created in :root:orgs:<new org> workspace
	invite := &pmcorev1alpha1.Invite{ObjectMeta: metav1.ObjectMeta{Name: wsName}}
	_, err = controllerutil.CreateOrUpdate(ctx, client, invite, func() error {
		invite.Spec.Email = creatorEmail

		return nil
	})
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to create invite resource %w", err)
	}

	log.Info().Str("workspace", wsName).Msg("invite resource is created")

	err = wait.ExponentialBackoffWithContext(ctx, retry.DefaultBackoff,
		func(ctx context.Context) (bool, error) {
			if err := client.Get(ctx, types.NamespacedName{Name: wsName}, invite); err != nil {
				return false, err
			}

			return meta.IsStatusConditionTrue(invite.GetConditions(), "Ready"), nil
		})
	if err != nil {
		log.Info().Str("workspace", wsName).Msg("invite resource not ready yet")
		//nolint:nilerr
		return subroutines.StopWithRequeue(w.limiter.When(lc),
			"invite resource is not ready yet"), nil
	}

	log.Info().Str("workspace", wsName).Msg("invite resource is ready")
	w.limiter.Forget(lc)
	return subroutines.OK(), nil
}

type keycloakUserResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func (w *inviteSubroutine) resolveCreatorEmail(ctx context.Context, userID string) (string, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/users/%s", w.keycloakBaseURL, w.creatorRealm, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	res, err := w.keycloak.Do(req)
	if err != nil {
		return "", fmt.Errorf("querying Keycloak user: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Keycloak returned status %s for user %s in realm %s", res.Status, userID, w.creatorRealm)
	}

	var user keycloakUserResponse
	if err := json.NewDecoder(res.Body).Decode(&user); err != nil {
		return "", fmt.Errorf("decoding Keycloak response: %w", err)
	}

	if user.Email == "" {
		return "", fmt.Errorf("Keycloak user %s in realm %s has no email", userID, w.creatorRealm)
	}

	log.Info().Str("userID", userID).Str("email", user.Email).Msg("resolved creator email from Keycloak")
	return user.Email, nil
}
