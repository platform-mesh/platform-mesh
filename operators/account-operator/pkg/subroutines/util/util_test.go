package util_test

import (
	"testing"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/util"
	"github.com/stretchr/testify/assert"
)

func TestGetWorkspaceTypeName(t *testing.T) {
	got := util.GetWorkspaceTypeName("test", v1alpha1.AccountTypeOrg)
	assert.Equal(t, "test-org", got)
}
