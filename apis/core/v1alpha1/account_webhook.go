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

package v1alpha1

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	mcruntime "sigs.k8s.io/multicluster-runtime"
)

func SetupAccountWebhookWithManager(mgr ctrl.Manager, organizationNameDenyList []string, accountTypeAllowList []AccountType) error {
	return mcruntime.NewWebhookManagedBy(mgr).
		For(&Account{}).
		WithDefaulter(&AccountDefaulter{}).
		WithValidator(&AccountValidator{OrganizationNameDenyList: organizationNameDenyList, AccountTypeAllowList: accountTypeAllowList}).
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

var _ webhook.CustomDefaulter = &AccountDefaulter{} //nolint:staticcheck
var _ webhook.CustomValidator = &AccountValidator{} //nolint:staticcheck

type AccountValidator struct {
	OrganizationNameDenyList []string
	AccountTypeAllowList     []AccountType
}

func (v *AccountValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	account := obj.(*Account)
	accountType := string(account.Spec.Type)

	if !slices.Contains(v.AccountTypeAllowList, account.Spec.Type) {
		WebhookValidations.WithLabelValues("create", "denied", accountType).Inc()
		return nil, fmt.Errorf("account type %s not in allowlist", account.Spec.Type)
	}

	if account.Spec.Type != AccountTypeOrg {
		WebhookValidations.WithLabelValues("create", "allowed", accountType).Inc()
		return nil, nil
	}

	if len(strings.TrimSpace(account.Name)) < 3 {
		WebhookValidations.WithLabelValues("create", "denied", accountType).Inc()
		return nil, fmt.Errorf("organization name %q is too short, must be at least 3 characters", account.Name)
	}

	if slices.Contains(v.OrganizationNameDenyList, account.Name) {
		WebhookValidations.WithLabelValues("create", "denied", accountType).Inc()
		return nil, fmt.Errorf("organization name %q is not allowed", account.Name)
	}

	WebhookValidations.WithLabelValues("create", "allowed", accountType).Inc()
	return nil, nil
}

func (v *AccountValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	account := newObj.(*Account)
	accountType := string(account.Spec.Type)

	if !slices.Contains(v.AccountTypeAllowList, account.Spec.Type) {
		WebhookValidations.WithLabelValues("update", "denied", accountType).Inc()
		return nil, fmt.Errorf("account type %s not in allowlist", account.Spec.Type)
	}

	if account.Spec.Type != AccountTypeOrg {
		WebhookValidations.WithLabelValues("update", "allowed", accountType).Inc()
		return nil, nil
	}

	if len(strings.TrimSpace(account.Name)) < 3 {
		WebhookValidations.WithLabelValues("update", "denied", accountType).Inc()
		return nil, fmt.Errorf("organization name %q is too short, must be at least 3 characters", account.Name)
	}

	if slices.Contains(v.OrganizationNameDenyList, account.Name) {
		WebhookValidations.WithLabelValues("update", "denied", accountType).Inc()
		return nil, fmt.Errorf("organization name %q is not allowed", account.Name)
	}

	WebhookValidations.WithLabelValues("update", "allowed", accountType).Inc()
	return nil, nil
}

func (v *AccountValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
