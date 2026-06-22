package subroutine

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func NewInviteSubroutine(mgr mcmanager.Manager, kcpClientGetter iclient.KCPClientGetter) (*inviteSubroutine, error) {
	lim, err := ratelimiter.NewStaticThenExponentialRateLimiter[*kcpcorev1alpha1.LogicalCluster](
		ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}
	return &inviteSubroutine{
		mgr:             mgr,
		kcpClientGetter: kcpClientGetter,
		limiter:         lim,
	}, nil
}

var (
	_ subroutines.Initializer = &inviteSubroutine{}
	_ subroutines.Processor   = &inviteSubroutine{}
)

type inviteSubroutine struct {
	mgr             mcmanager.Manager
	kcpClientGetter iclient.KCPClientGetter
	limiter         workqueue.TypedRateLimiter[*kcpcorev1alpha1.LogicalCluster]
}

func (w *inviteSubroutine) GetName() string { return "InviteInitializationSubroutine" }

// Initialize implements subroutines.Initializer.
func (w *inviteSubroutine) Initialize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return w.reconcile(ctx, obj)
}

// Process implements subroutines.Processor.
func (w *inviteSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return w.reconcile(ctx, obj)
}

func (w *inviteSubroutine) reconcile(ctx context.Context, obj client.Object) (subroutines.Result, error) {
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
	var account corev1alpha1.Account
	err = orgsClient.Get(ctx, types.NamespacedName{Name: wsName}, &account)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get account resource %w", err)
	}

	if account.Spec.Type != corev1alpha1.AccountTypeOrg {
		log.Info().Str("workspace", wsName).Msg("account is not of type organization, skipping invite creation")
		return subroutines.OK(), nil
	}

	if account.Spec.Creator == nil {
		log.Info().Str("workspace", wsName).Msg("account creator is nil, skipping invite creation")
		return subroutines.OK(), nil
	}

	// the Invite resource is created in :root:orgs:<new org> workspace
	invite := &corev1alpha1.Invite{ObjectMeta: metav1.ObjectMeta{Name: wsName}}
	_, err = controllerutil.CreateOrUpdate(ctx, client, invite, func() error {
		invite.Spec.Email = *account.Spec.Creator

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
		return subroutines.StopWithRequeue(w.limiter.When(lc),
			"invite resource is not ready yet"), nil
	}

	log.Info().Str("workspace", wsName).Msg("invite resource is ready")
	w.limiter.Forget(lc)
	return subroutines.OK(), nil
}
