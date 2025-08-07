package v1alpha1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAccountValidator_ValidateCreate(t *testing.T) {
	tests := []struct {
		name        string
		account     *Account
		denyList    []string
		expectError bool
		errorMsg    string
	}{
		{
			name: "denied organization name",
			account: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "org"},
			},
			denyList:    []string{"admin", "root", "system"},
			expectError: true,
			errorMsg:    `organization name "admin" is not allowed`,
		},
		{
			name: "allowed organization name",
			account: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "my-org"},
				Spec:       AccountSpec{Type: "org"},
			},
			denyList:    []string{"admin", "root", "system"},
			expectError: false,
		},
		{
			name: "non-organization account with denied name",
			account: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "personal"},
			},
			denyList:    []string{"admin", "root", "system"},
			expectError: false,
		},
		{
			name: "empty deny list",
			account: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "org"},
			},
			denyList:    []string{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &AccountValidator{DenyList: tt.denyList}
			_, err := validator.ValidateCreate(context.Background(), tt.account)

			if tt.expectError {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAccountValidator_ValidateUpdate(t *testing.T) {
	tests := []struct {
		name        string
		oldAccount  *Account
		newAccount  *Account
		denyList    []string
		expectError bool
		errorMsg    string
	}{
		{
			name: "organization name in deny list on update",
			oldAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "my-org"},
				Spec:       AccountSpec{Type: "org"},
			},
			newAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "org"},
			},
			denyList:    []string{"admin", "root", "system"},
			expectError: true,
			errorMsg:    `organization name "admin" is not allowed`,
		},
		{
			name: "allowed organization name on update",
			oldAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "my-org"},
				Spec:       AccountSpec{Type: "org"},
			},
			newAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "updated-org"},
				Spec:       AccountSpec{Type: "org"},
			},
			denyList:    []string{"admin", "root", "system"},
			expectError: false,
		},
		{
			name: "changing from personal to organization with denied name",
			oldAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "personal"},
			},
			newAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "org"},
			},
			denyList:    []string{"admin", "root", "system"},
			expectError: true,
			errorMsg:    `organization name "admin" is not allowed`,
		},
		{
			name: "changing from personal to organization with allowed name",
			oldAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "my-account"},
				Spec:       AccountSpec{Type: "personal"},
			},
			newAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "my-account"},
				Spec:       AccountSpec{Type: "org"},
			},
			denyList:    []string{"admin", "root", "system"},
			expectError: false,
		},
		{
			name: "changing from organization to personal with denied name",
			oldAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "org"},
			},
			newAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "personal"},
			},
			denyList:    []string{"admin", "root", "system"},
			expectError: false,
		},
		{
			name: "updating personal account with denied name",
			oldAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "personal"},
			},
			newAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "personal"},
			},
			denyList:    []string{"admin", "root", "system"},
			expectError: false,
		},
		{
			name: "empty deny list",
			oldAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "my-org"},
				Spec:       AccountSpec{Type: "org"},
			},
			newAccount: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "org"},
			},
			denyList:    []string{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &AccountValidator{DenyList: tt.denyList}
			_, err := validator.ValidateUpdate(context.Background(), tt.oldAccount, tt.newAccount)

			if tt.expectError {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
