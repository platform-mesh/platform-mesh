package subroutine

import (
	"context"
	"fmt"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	accountv1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/rs/zerolog/log"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/platform-mesh/golang-commons/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"k8s.io/client-go/util/retry"

	"github.com/platform-mesh/security-operator/api/v1alpha1"
)

func NewInviteSubroutine(orgsClient client.Client, mgr mcmanager.Manager) *inviteSubroutine {
	return &inviteSubroutine{
		orgsClient: orgsClient,
		mgr:        mgr,
	}
}

var _ lifecyclesubroutine.Subroutine = &inviteSubroutine{}

type inviteSubroutine struct {
	orgsClient client.Client
	mgr        mcmanager.Manager
}

func (w *inviteSubroutine) Finalize(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	return ctrl.Result{}, nil
}

func (w *inviteSubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string {
	return nil
}

func (w *inviteSubroutine) GetName() string { return "InviteInitilizationSubroutine" }

func (w *inviteSubroutine) Process(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	lc := instance.(*kcpv1alpha1.LogicalCluster)

	wsName := getWorkspaceName(lc)
	if wsName == "" {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get workspace name"), true, false)
	}

	cl, err := w.mgr.ClusterFromContext(ctx)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get cluster from context %w", err), true, true)
	}

	var account accountv1alpha1.Account
	err = w.orgsClient.Get(ctx, types.NamespacedName{Name: wsName}, &account)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get account resource %w", err), true, true)
	}

	if account.Spec.Type != accountv1alpha1.AccountTypeOrg {
		log.Info().Str("workspace", wsName).Msg("account is not of type organization, skipping invite creation")
		return ctrl.Result{}, nil
	}

	if account.Spec.Creator == nil {
		log.Info().Str("workspace", wsName).Msg("account creator is nil, skipping invite creation")
		return ctrl.Result{}, nil
	}

	// the Invite resource is created in :root:orgs:<new org> workspace
	invite := &v1alpha1.Invite{ObjectMeta: metav1.ObjectMeta{Name: wsName}}
	_, err = controllerutil.CreateOrUpdate(ctx, cl.GetClient(), invite, func() error {
		invite.Spec.Email = *account.Spec.Creator

		return nil
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to create invite resource %w", err), true, true)
	}

	log.Info().Str("workspace", wsName).Msg("invite resource is created")

	err = wait.ExponentialBackoffWithContext(ctx, retry.DefaultBackoff,
		func(ctx context.Context) (bool, error) {
			if err := cl.GetClient().Get(ctx, types.NamespacedName{Name: wsName}, invite); err != nil {
				return false, err
			}

			return meta.IsStatusConditionTrue(invite.GetConditions(), "Ready"), nil
		})
	if err != nil {
		log.Info().Str("workspace", wsName).Msg("invite resource not ready yet")
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("invite resource is not ready yet"), true, false)
	}

	log.Info().Str("workspace", wsName).Msg("invite resource is ready")
	return ctrl.Result{}, nil
}
