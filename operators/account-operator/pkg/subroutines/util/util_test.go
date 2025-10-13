package util_test

import (
	"testing"

	"github.com/platform-mesh/account-operator/pkg/subroutines/util"
	"github.com/stretchr/testify/assert"
)

func TestGetOrgWorkspaceTypeName(t *testing.T) {
	got := util.GetOrgWorkspaceTypeName("test")
	assert.Equal(t, "test-org", got)
}

func TestGetAccountWorkspaceTypeName(t *testing.T) {
	got := util.GetAccountWorkspaceTypeName("test")
	assert.Equal(t, "test-acc", got)
}
