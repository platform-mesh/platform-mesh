package util

import "fmt"

func GetOrgWorkspaceTypeName(accountName string) string {
	return fmt.Sprintf("%s-org", accountName)
}

func GetAccountWorkspaceTypeName(accountName string) string {
	return fmt.Sprintf("%s-acc", accountName)
}
