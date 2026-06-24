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
	"errors"
	"fmt"
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
)

type BaseTuplesInput struct {
	Creator                string
	AccountOriginClusterID string
	AccountName            string
	CreatorRelation        string
	ObjectType             string
}

type TuplesForOrganizationInput struct {
	BaseTuplesInput
}

type InitialTuplesForAccountInput struct {
	BaseTuplesInput
	ParentOriginClusterID string
	ParentName            string
	ParentRelation        string
}

// InitialTuplesForAccount returns FGA tuples for an account not of type
// organization.
func InitialTuplesForAccount(in InitialTuplesForAccountInput) ([]pmcorev1alpha1.Tuple, error) {
	base, err := baseTuples(in.BaseTuplesInput)
	if err != nil {
		return nil, err
	}
	tuples := append(base, pmcorev1alpha1.Tuple{
		User:     renderAccountEntity(in.ObjectType, in.ParentOriginClusterID, in.ParentName),
		Relation: in.ParentRelation,
		Object:   renderAccountEntity(in.ObjectType, in.AccountOriginClusterID, in.AccountName),
	})
	return tuples, nil
}

// TuplesForOrganization returns FGA tuples for an Account of type organization.
func TuplesForOrganization(in TuplesForOrganizationInput) ([]pmcorev1alpha1.Tuple, error) {
	return baseTuples(in.BaseTuplesInput)
}

// IsTupleOfAccountFilter returns a filter determining whether a tuple is tied
// to the given account, i.e. contains its cluster id.
func IsTupleOfAccountFilter(generatedClusterID string) TupleFilter {
	return func(t pmcorev1alpha1.Tuple) bool {
		return generatedClusterID != "" && (strings.Contains(t.Object, generatedClusterID) || strings.Contains(t.User, generatedClusterID))
	}
}

// ReferencingAccountTupleKey returns a key that can be used to List tuples that
// reference a given account.
func ReferencingAccountTupleKey(objectType, accountOriginClusterID, accountName string) *openfgav1.ReadRequestTupleKey {
	return &openfgav1.ReadRequestTupleKey{
		Object: renderAccountEntity(objectType, accountOriginClusterID, accountName),
	}
}

// ReferencingOwnerRoleTupleKey returns a key that can be used to List tuples
// that reference the owner role of a given account.
func ReferencingOwnerRoleTupleKey(objectType, accountOriginClusterID, accountName string) *openfgav1.ReadRequestTupleKey {
	return &openfgav1.ReadRequestTupleKey{
		Object: renderOwnerRole(objectType, accountOriginClusterID, accountName),
	}
}

func baseTuples(in BaseTuplesInput) ([]pmcorev1alpha1.Tuple, error) {
	if in.Creator == "" {
		return nil, errors.New("account creator is empty")
	}

	return []pmcorev1alpha1.Tuple{
		{
			User:     renderCreatorUser(in.Creator),
			Relation: "assignee",
			Object:   renderOwnerRole(in.ObjectType, in.AccountOriginClusterID, in.AccountName),
		},
		{
			User:     renderOwnerRoleAssigneeGroup(in.ObjectType, in.AccountOriginClusterID, in.AccountName),
			Relation: in.CreatorRelation,
			Object:   renderAccountEntity(in.ObjectType, in.AccountOriginClusterID, in.AccountName),
		},
	}, nil
}

// formatUser formats a user to be stored in an FGA tuple, i.e. replaces colons
// with dots.
func formatUser(user string) string {
	return strings.ReplaceAll(user, ":", ".")
}

func renderAccountEntity(objectType, originClusterID, name string) string {
	return fmt.Sprintf("%s:%s/%s", objectType, originClusterID, name)
}

func renderCreatorUser(creator string) string {
	return fmt.Sprintf("user:%s", formatUser(creator))
}

// RenderRolePrefix returns the prefix for role User strings that reference an
// Account's roles (e.g. "role:objectType/originClusterID/name/").
func RenderRolePrefix(objectType, originClusterID, name string) string {
	return fmt.Sprintf("role:%s/%s/%s/", objectType, originClusterID, name)
}

func renderOwnerRole(objectType, originClusterID, name string) string {
	return RenderRolePrefix(objectType, originClusterID, name) + "owner"
}

func renderOwnerRoleAssigneeGroup(objectType, originClusterID, name string) string {
	return RenderRolePrefix(objectType, originClusterID, name) + "owner#assignee"
}
