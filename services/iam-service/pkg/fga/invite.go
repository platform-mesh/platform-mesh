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

package fga

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/mail"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	securityv1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/fga/util"
	"go.platform-mesh.io/golang-commons/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"go.platform-mesh.io/iam-service/pkg/graph"
	"go.platform-mesh.io/iam-service/pkg/roles"
)

// emailToLabelValue converts an email address to a valid Kubernetes label value
// by creating a SHA-1 hash. This ensures the value meets Kubernetes label requirements:
// - 63 characters or less
// - alphanumeric characters only
func emailToLabelValue(email string) string {
	hash := sha1.Sum([]byte(email))
	return hex.EncodeToString(hash[:])
}

// checkAndInviteUser checks if a user exists in the IDM system and creates an Invite if not
func (s *Service) checkAndInviteUser(ctx context.Context, userEmail string, rctx graph.ResourceContext) error {
	log := logger.LoadLoggerFromContext(ctx).MustChildLoggerWithAttributes("email", sanitizeUserID(userEmail))

	// Check if user exists in IDM system
	usr, err := s.idmChecker.UserByMail(ctx, userEmail)
	if err != nil {
		return errors.Wrap(err, "failed to check if user %s exists in IDM system", sanitizeUserID(userEmail))
	}

	if usr != nil {
		// User exists, no invite needed
		return nil
	}

	log.Debug().Msg("User not found in IDM system, will create Invite")

	// User doesn't exist, need to create an Invite in the account workspace
	// Get workspace client for the account path
	path := rctx.AccountPath
	if rctx.Group == "core.platform-mesh.io" && rctx.Kind == "Account" {
		path = fmt.Sprintf("%s:%s", path, rctx.Resource.Name)
	}
	wsClient, err := s.wsClientFactory.New(ctx, multicluster.ClusterName(path))
	if err != nil {
		return errors.Wrap(err, "failed to create workspace client for path %s", path)
	}

	if err := s.createInviteIfNotExists(ctx, wsClient, userEmail); err != nil {
		return errors.Wrap(err, "failed to create Invite for user %s", sanitizeUserID(userEmail))
	}

	return nil
}

// createInviteIfNotExists creates or updates an Invite resource for the user
func (s *Service) createInviteIfNotExists(ctx context.Context, wsClient client.Client, userEmail string) error {
	// Validate email format
	if _, err := mail.ParseAddress(userEmail); err != nil {
		return errors.Wrap(err, "invalid email format for %s", sanitizeUserID(userEmail))
	}

	log := logger.LoadLoggerFromContext(ctx).MustChildLoggerWithAttributes("email", sanitizeUserID(userEmail))

	// Check if an Invite already exists for this email using label selector
	// Use hash of email as label value since email contains invalid characters (@, .)
	emailHash := emailToLabelValue(userEmail)
	inviteList := &securityv1alpha1.InviteList{}
	labelSelector := client.MatchingLabels{
		"platform-mesh.io/invite-email-hash": emailHash,
	}
	if err := wsClient.List(ctx, inviteList, labelSelector); err != nil { // coverage-ignore
		return errors.Wrap(err, "failed to list existing Invites for %s", sanitizeUserID(userEmail))
	}

	// If invite already exists, return early
	if len(inviteList.Items) > 0 {
		log.Debug().Str("inviteName", inviteList.Items[0].Name).Msg("Invite already exists")
		return nil
	}

	// Create new Invite with label
	invite := &securityv1alpha1.Invite{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "invite-",
			Labels: map[string]string{
				"platform-mesh.io/invite-email-hash": emailHash,
			},
		},
		Spec: securityv1alpha1.InviteSpec{
			Email: userEmail,
		},
	}

	if err := wsClient.Create(ctx, invite); err != nil { // coverage-ignore
		return errors.Wrap(err, "failed to create Invite resource for %s", sanitizeUserID(userEmail))
	}

	log.Info().Str("inviteName", invite.Name).Msg("Successfully created Invite resource")
	return nil
}

// processInvites processes invite requests: checks if users exist, creates Invite resources if not, and assigns roles
func (s *Service) processInvites(ctx context.Context, rctx graph.ResourceContext, invites []*graph.InviteInput, storeID, fgaTypeName, clusterId string, log *logger.Logger) (int, []string) {
	var inviteErrors []string
	var assignedCount int

	for _, invite := range invites {
		inviteLog := log.MustChildLoggerWithAttributes("email", sanitizeUserID(invite.Email))
		inviteLog.Debug().Interface("roles", invite.Roles).Msg("Processing invite")

		// Check if user exists in IDM system and create Invite if not
		if err := s.checkAndInviteUser(ctx, invite.Email, rctx); err != nil {
			errMsg := fmt.Sprintf("failed to create invite for user '%s': %v", sanitizeUserID(invite.Email), err)
			inviteErrors = append(inviteErrors, errMsg)
			inviteLog.Warn().Err(err).Msg("Failed to create Invite for user, continuing with role assignment")
		}

		// Assign roles to the invited user (using email as userID)
		// Validate that only available roles are being assigned
		roleDefinitions, err := s.rolesRetriever.GetRoleDefinitions(rctx)
		if err != nil { // coverage-ignore: difficult to test without mocking - requires nil config
			errMsg := fmt.Sprintf("failed to get role definitions for group resource '%s/%s': %v", rctx.Group, rctx.Kind, err)
			inviteErrors = append(inviteErrors, errMsg)
			log.Error().Err(err).Msg("Failed to retrieve role definitions")
			continue
		}
		availableRoles := roles.GetAvailableRoleIDs(roleDefinitions)

		for _, role := range invite.Roles {
			roleLog := inviteLog.MustChildLoggerWithAttributes("role", role)
			if !containsString(availableRoles, role) {
				errMsg := fmt.Sprintf("role '%s' is not allowed for user '%s'. Only roles %v are permitted", role, sanitizeUserID(invite.Email), availableRoles)
				inviteErrors = append(inviteErrors, errMsg)
				roleLog.Warn().Interface("availableRoles", availableRoles).Msg("Invalid role assignment attempted")
				continue
			}

			// Create the role tuple and assign role tuple for this user-role combination
			count, errs := s.assignRoleToUser(ctx, invite.Email, role, rctx, storeID, fgaTypeName, clusterId, roleLog)
			assignedCount += count
			inviteErrors = append(inviteErrors, errs...)
		}
	}

	return assignedCount, inviteErrors
}

// assignRoleToUser assigns a single role to a user by creating both the role assignment tuple and the permission tuple
func (s *Service) assignRoleToUser(ctx context.Context, userEmail, role string, rctx graph.ResourceContext, storeID, fgaTypeName, clusterId string, log *logger.Logger) (int, []string) {
	var errors []string
	var assignedCount int

	// Create the role assignment tuple (user -> role)
	roleTuple := &openfgav1.TupleKey{
		User:     fmt.Sprintf("user:%s", userEmail),
		Relation: "assignee",
		Object: fmt.Sprintf("role:%s/%s/%s/%s",
			fgaTypeName,
			clusterId,
			rctx.Resource.Name,
			role),
	}

	// Create the permission tuple (role -> resource)
	targetFGATypeName := util.ConvertToTypeName(rctx.Group, rctx.Kind)
	targetObject := fmt.Sprintf("%s:%s/%s", targetFGATypeName, clusterId, rctx.Resource.Name)
	if rctx.Resource.Namespace != nil {
		targetObject = fmt.Sprintf("%s:%s/%s/%s", targetFGATypeName, clusterId, *rctx.Resource.Namespace, rctx.Resource.Name)
	}
	assignRoleTuple := &openfgav1.TupleKey{
		User: fmt.Sprintf("role:%s/%s/%s/%s#assignee",
			fgaTypeName,
			clusterId,
			rctx.Resource.Name,
			role),
		Relation: role,
		Object:   targetObject,
	}

	// Write both tuples
	for _, write := range []*openfgav1.TupleKey{roleTuple, assignRoleTuple} {
		writeReq := &openfgav1.WriteRequest{
			StoreId: storeID,
			Writes: &openfgav1.WriteRequestWrites{
				TupleKeys: []*openfgav1.TupleKey{write},
			},
		}

		_, err := s.client.Write(ctx, writeReq)
		if err != nil {
			if isDuplicateWriteError(err) {
				log.Info().Str("relation", write.Relation).Str("object", write.Object).Msg("Tuple already exists, skipping duplicate")
			} else { // coverage-ignore
				errMsg := fmt.Sprintf("failed to assign role '%s' to user '%s': %v", role, sanitizeUserID(userEmail), err)
				errors = append(errors, errMsg)
				log.Error().Err(err).Msg("Failed to write tuple to FGA")
			}
		} else {
			assignedCount++
			log.Info().Msg("Successfully assigned role to user")
		}
	}

	return assignedCount, errors
}
