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

// AccountKey is the structured form of the two FGA key shapes that embed an
// account's origin cluster id:
//
//	<objectType>:<clusterID>/<name>[#<relation>]
//	role:<objectType>/<clusterID>/<name>/<role>[#<relation>]
//
// It is the single source of truth for that format: every renderer below
// builds keys through String, and ParseAccountKey reads them back, so the two
// directions cannot drift apart (round-trip asserted in the tests).
type AccountKey struct {
	ObjectType string
	ClusterID  string
	Name       string
	Role       string // non-empty for the role shape
	Relation   string // optional "#<relation>" suffix (e.g. "assignee")
}

// String renders the key into its tuple representation. It round-trips with
// ParseAccountKey.
func (k AccountKey) String() string {
	var s string
	if k.Role != "" {
		s = k.RolePrefix() + k.Role
	} else {
		s = fmt.Sprintf("%s:%s/%s", k.ObjectType, k.ClusterID, k.Name)
	}
	if k.Relation != "" {
		s += "#" + k.Relation
	}
	return s
}

// RolePrefix returns the prefix shared by all role keys of this account
// (e.g. "role:objectType/clusterID/name/").
func (k AccountKey) RolePrefix() string {
	return fmt.Sprintf("role:%s/%s/%s/", k.ObjectType, k.ClusterID, k.Name)
}

// ParseAccountKey parses s into an AccountKey. It returns false for strings
// that do not match either shape rendered by AccountKey.String (e.g.
// "user:someone", "role:authenticated#assignee").
//
// The match is purely structural: other key families share the raw
// `<type>:<clusterID>/<name>` shape (e.g. the
// `apis_kcp_io_apiexport:<providerClusterID>/<exportName>` user keys written
// into the same org store for APIExportPolicies), so callers MUST additionally
// check ObjectType against the configured account object type before treating
// the result as an account key.
func ParseAccountKey(s string) (AccountKey, bool) {
	var k AccountKey

	if i := strings.IndexByte(s, '#'); i >= 0 {
		k.Relation = s[i+1:]
		s = s[:i]
		if k.Relation == "" {
			return AccountKey{}, false
		}
	}

	typ, rest, found := strings.Cut(s, ":")
	if !found || typ == "" || typ == "user" {
		return AccountKey{}, false
	}

	parts := strings.Split(rest, "/")
	switch {
	case typ == "role" && len(parts) == 4:
		k.ObjectType, k.ClusterID, k.Name, k.Role = parts[0], parts[1], parts[2], parts[3]
	case typ != "role" && len(parts) == 2:
		k.ObjectType, k.ClusterID, k.Name = typ, parts[0], parts[1]
	default:
		return AccountKey{}, false
	}

	if k.ObjectType == "" || k.ClusterID == "" || k.Name == "" || (typ == "role" && k.Role == "") {
		return AccountKey{}, false
	}

	return k, true
}

func renderAccountEntity(objectType, originClusterID, name string) string {
	return AccountKey{ObjectType: objectType, ClusterID: originClusterID, Name: name}.String()
}

func renderCreatorUser(creator string) string {
	return fmt.Sprintf("user:%s", formatUser(creator))
}

// RenderRolePrefix returns the prefix for role User strings that reference an
// Account's roles (e.g. "role:objectType/originClusterID/name/").
func RenderRolePrefix(objectType, originClusterID, name string) string {
	return AccountKey{ObjectType: objectType, ClusterID: originClusterID, Name: name}.RolePrefix()
}

func renderOwnerRole(objectType, originClusterID, name string) string {
	return AccountKey{ObjectType: objectType, ClusterID: originClusterID, Name: name, Role: "owner"}.String()
}

func renderOwnerRoleAssigneeGroup(objectType, originClusterID, name string) string {
	return AccountKey{
		ObjectType: objectType,
		ClusterID:  originClusterID,
		Name:       name,
		Role:       "owner",
		Relation:   "assignee",
	}.String()
}
