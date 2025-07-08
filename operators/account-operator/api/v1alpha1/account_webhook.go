package v1alpha1

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func SetupAccountWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&Account{}).
		WithDefaulter(&AccountDefaulter{}).
		Complete()
}

type AccountDefaulter struct{}

// Default implements admission.CustomDefaulter.
func (a *AccountDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	account := obj.(*Account)

	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return err
	}

	account.Spec.Creator = &req.UserInfo.Username

	return nil
}

var _ webhook.CustomDefaulter = &AccountDefaulter{}
