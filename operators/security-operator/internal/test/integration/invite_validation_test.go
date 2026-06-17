package test

import (
	"fmt"
	"strings"
	"testing"

	securityv1alpha1 "github.com/platform-mesh/platform-mesh/apis/core/v1alpha1"
	"github.com/stretchr/testify/require"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (suite *IntegrationSuite) TestInviteEmailValidation() {
	ctx := suite.T().Context()

	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		// Valid examples (should pass both OpenAPI `format: email` and the Keycloak-aligned pattern).
		{name: "simple", email: "user@example.com"},
		{name: "plus-tag", email: "user+tag@example.com"},
		{name: "dot-in-local", email: "first.last@example.com"},
		{name: "subdomain", email: "user@mail.example.com"},
		{name: "dash-in-domain", email: "user@my-domain.example"},
		{name: "allowed-specials-in-local", email: "a!#$%&'*+/=?^_`{|}~.-b@example.com"},

		// Invalid examples.
		{name: "missing-at", email: "not-an-email", wantErr: true},
		{name: "empty-local", email: "@example.com", wantErr: true},
		{name: "empty-domain", email: "user@", wantErr: true},
		{name: "space", email: "user name@example.com", wantErr: true},
		{name: "multiple-at", email: "user@@example.com", wantErr: true},
		{name: "double-dot-domain", email: "user@example..com", wantErr: true},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			invite := &securityv1alpha1.Invite{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "email-validation-" + strings.ToLower(tt.name) + "-",
				},
				Spec: securityv1alpha1.InviteSpec{Email: tt.email},
			}

			err := suite.platformMeshSystemClient.Create(ctx, invite)
			if tt.wantErr {
				require.Error(t, err)
				require.Truef(
					t,
					kerrors.IsInvalid(err) || kerrors.IsBadRequest(err),
					"expected validation error when creating Invite with invalid spec.email, got: %v",
					err,
				)
				return
			}

			require.NoError(t, err)
			t.Cleanup(func() {
				if err := suite.platformMeshSystemClient.Delete(ctx, invite); err != nil && !kerrors.IsNotFound(err) {
					t.Logf("failed to delete Invite %q: %v", invite.Name, err)
				}
			})
			require.NotEmpty(t, invite.Name, fmt.Sprintf("expected server to assign name for Invite, got: %q", invite.Name))
		})
	}
}
