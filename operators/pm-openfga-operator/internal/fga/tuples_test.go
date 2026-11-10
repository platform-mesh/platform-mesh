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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
)

const (
	accountName        = "one"
	parentAccountName  = "default"
	generatedClusterID = "1mj722nrt4jo3ggn"
	originClusterID    = "14uc34987epvgggc"
	creator            = "new@example.com"
	creatorRelation    = "owner"
	parentRelation     = "parent"
	objectType         = "core_platform-mesh_io_account"
)

func TestInitialTuplesForAccount(t *testing.T) {
	in := InitialTuplesForAccountInput{
		BaseTuplesInput: BaseTuplesInput{
			Creator:                creator,
			AccountOriginClusterID: originClusterID,
			AccountName:            accountName,
			CreatorRelation:        creatorRelation,
			ObjectType:             objectType,
		},
		ParentOriginClusterID: originClusterID,
		ParentName:            parentAccountName,
		ParentRelation:        parentRelation,
	}
	tuples, err := InitialTuplesForAccount(in)
	require.NoError(t, err)
	require.Len(t, tuples, 3)

	// Tuple 1: creator gets assignee on owner role
	assert.Equal(t, pmcorev1alpha1.Tuple{
		Object:   "role:core_platform-mesh_io_account/14uc34987epvgggc/one/owner",
		Relation: "assignee",
		User:     "user:new@example.com",
	}, tuples[0])

	// Tuple 2: owner role has creator relation on account
	assert.Equal(t, pmcorev1alpha1.Tuple{
		Object:   "core_platform-mesh_io_account:14uc34987epvgggc/one",
		Relation: "owner",
		User:     "role:core_platform-mesh_io_account/14uc34987epvgggc/one/owner#assignee",
	}, tuples[1])

	// Tuple 3: parent account has parent relation on account
	assert.Equal(t, pmcorev1alpha1.Tuple{
		Object:   "core_platform-mesh_io_account:14uc34987epvgggc/one",
		Relation: "parent",
		User:     "core_platform-mesh_io_account:14uc34987epvgggc/default",
	}, tuples[2])
}

func TestInitialTuplesForAccount_formatUser(t *testing.T) {
	tests := []struct {
		name    string
		creator string
		want    string
	}{
		{
			name:    "service account replaces colons",
			creator: "system:serviceaccount:ns:name",
			want:    "user:system.serviceaccount.ns.name",
		},
		{
			name:    "email preserves dots",
			creator: "john.doe@example.com",
			want:    "user:john.doe@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := InitialTuplesForAccountInput{
				BaseTuplesInput: BaseTuplesInput{
					Creator:                tt.creator,
					AccountOriginClusterID: originClusterID,
					AccountName:            accountName,
					CreatorRelation:        creatorRelation,
					ObjectType:             objectType,
				},
				ParentOriginClusterID: originClusterID,
				ParentName:            parentAccountName,
				ParentRelation:        parentRelation,
			}
			tuples, err := InitialTuplesForAccount(in)
			require.NoError(t, err)
			require.Len(t, tuples, 3)

			assert.Equal(t, tt.want, tuples[0].User)
		})
	}
}

func TestInitialTuplesForAccount_nilCreator(t *testing.T) {
	in := InitialTuplesForAccountInput{
		BaseTuplesInput: BaseTuplesInput{
			Creator:                "",
			AccountOriginClusterID: originClusterID,
			AccountName:            accountName,
			CreatorRelation:        creatorRelation,
			ObjectType:             objectType,
		},
		ParentOriginClusterID: originClusterID,
		ParentName:            parentAccountName,
		ParentRelation:        parentRelation,
	}
	_, err := InitialTuplesForAccount(in)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "creator is empty")
}

func TestParseAccountKey(t *testing.T) {
	tests := []struct {
		in   string
		want AccountKey
		ok   bool
	}{
		{
			in:   "core_platform-mesh_io_account:abc123/myorg",
			want: AccountKey{ObjectType: "core_platform-mesh_io_account", ClusterID: "abc123", Name: "myorg"},
			ok:   true,
		},
		{
			in:   "role:core_platform-mesh_io_account/abc123/myorg/owner",
			want: AccountKey{ObjectType: "core_platform-mesh_io_account", ClusterID: "abc123", Name: "myorg", Role: "owner"},
			ok:   true,
		},
		{
			in:   "role:core_platform-mesh_io_account/abc123/myorg/owner#assignee",
			want: AccountKey{ObjectType: "core_platform-mesh_io_account", ClusterID: "abc123", Name: "myorg", Role: "owner", Relation: "assignee"},
			ok:   true,
		},
		{
			in:   "core_platform-mesh_io_account:abc123/myorg#member",
			want: AccountKey{ObjectType: "core_platform-mesh_io_account", ClusterID: "abc123", Name: "myorg", Relation: "member"},
			ok:   true,
		},
		// Non-account keys must not parse.
		{in: "user:someone.example.com", ok: false},
		{in: "user:*", ok: false},
		{in: "role:authenticated", ok: false},
		{in: "role:authenticated#assignee", ok: false},
		{in: "no-colon-here", ok: false},
		{in: "core_platform-mesh_io_account:abc123", ok: false},
		{in: "core_platform-mesh_io_account:abc123/a/b", ok: false},
		{in: "role:type/cluster/name", ok: false},
		{in: "role:type/cluster/name/role/extra", ok: false},
		{in: "role:type//name/owner", ok: false},
		{in: ":cluster/name", ok: false},
		{in: "core_platform-mesh_io_account:abc123/myorg#", ok: false},
	}
	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			got, ok := ParseAccountKey(test.in)
			assert.Equal(t, test.ok, ok)
			if test.ok {
				assert.Equal(t, test.want, got)
				// Round-trip.
				assert.Equal(t, test.in, got.String())
			}
		})
	}
}

// TestRenderersRoundTripThroughAccountKey pins the renderers and the parser to
// each other: every key the package writes must parse back into the fields it
// was built from. Changing one direction without the other fails here.
func TestRenderersRoundTripThroughAccountKey(t *testing.T) {
	const (
		objectType = "core_platform-mesh_io_account"
		clusterID  = "abc123"
		name       = "myorg"
	)
	tests := []struct {
		name     string
		rendered string
		want     AccountKey
	}{
		{
			name:     "renderAccountEntity",
			rendered: renderAccountEntity(objectType, clusterID, name),
			want:     AccountKey{ObjectType: objectType, ClusterID: clusterID, Name: name},
		},
		{
			name:     "renderOwnerRole",
			rendered: renderOwnerRole(objectType, clusterID, name),
			want:     AccountKey{ObjectType: objectType, ClusterID: clusterID, Name: name, Role: "owner"},
		},
		{
			name:     "renderOwnerRoleAssigneeGroup",
			rendered: renderOwnerRoleAssigneeGroup(objectType, clusterID, name),
			want:     AccountKey{ObjectType: objectType, ClusterID: clusterID, Name: name, Role: "owner", Relation: "assignee"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ParseAccountKey(test.rendered)
			require.True(t, ok, "renderer output %q must parse as an account key", test.rendered)
			assert.Equal(t, test.want, got)
			assert.Equal(t, test.rendered, got.String())
		})
	}

	// RenderRolePrefix is a prefix, not a whole key: every role key starts with it.
	prefix := RenderRolePrefix(objectType, clusterID, name)
	assert.Equal(t, prefix, AccountKey{ObjectType: objectType, ClusterID: clusterID, Name: name}.RolePrefix())
	assert.True(t, strings.HasPrefix(renderOwnerRole(objectType, clusterID, name), prefix))
}
