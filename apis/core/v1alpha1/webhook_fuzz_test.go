package v1alpha1

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func FuzzAccountValidateCreate(f *testing.F) {
	f.Add("my-org", "org", "admin,root,system")
	f.Add("ab", "org", "")
	f.Add("", "", "blocked")
	f.Add("valid-name", "account", "")
	f.Add("a", "unknown-type", "a")

	f.Fuzz(func(t *testing.T, name, accountType, denyListCSV string) {
		var denyList []string
		if denyListCSV != "" {
			denyList = splitCSV(denyListCSV)
		}

		account := &Account{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       AccountSpec{Type: AccountType(accountType)},
		}
		validator := &AccountValidator{
			OrganizationNameDenyList: denyList,
			AccountTypeAllowList:     []AccountType{AccountTypeAccount, AccountTypeOrg},
		}

		// Must not panic — validation errors are expected
		_, _ = validator.ValidateCreate(context.Background(), account)
	})
}

func FuzzAccountValidateUpdate(f *testing.F) {
	f.Add("old-org", "org", "new-org", "org", "admin,root")
	f.Add("admin", "account", "admin", "org", "admin")
	f.Add("", "", "", "", "")

	f.Fuzz(func(t *testing.T, oldName, oldType, newName, newType, denyListCSV string) {
		var denyList []string
		if denyListCSV != "" {
			denyList = splitCSV(denyListCSV)
		}

		oldAccount := &Account{
			ObjectMeta: metav1.ObjectMeta{Name: oldName},
			Spec:       AccountSpec{Type: AccountType(oldType)},
		}
		newAccount := &Account{
			ObjectMeta: metav1.ObjectMeta{Name: newName},
			Spec:       AccountSpec{Type: AccountType(newType)},
		}
		validator := &AccountValidator{
			OrganizationNameDenyList: denyList,
			AccountTypeAllowList:     []AccountType{AccountTypeAccount, AccountTypeOrg},
		}

		// Must not panic — validation errors are expected
		_, _ = validator.ValidateUpdate(context.Background(), oldAccount, newAccount)
	})
}

func splitCSV(s string) []string {
	var result []string
	start := 0
	for i := range len(s) {
		if s[i] == ',' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}
