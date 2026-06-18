package util

import (
	"fmt"

	"github.com/platform-mesh/platform-mesh/apis/core/v1alpha1"
)

func GetWorkspaceTypeName(accountName string, accountType v1alpha1.AccountType) string {
	return fmt.Sprintf("%s-%s", accountName, accountType)
}
