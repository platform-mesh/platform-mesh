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
	in := InitialTuplesForAccountInput{
		BaseTuplesInput: BaseTuplesInput{
			Creator:                "system:serviceaccount:ns:name",
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

	assert.Equal(t, "user:system.serviceaccount.ns.name", tuples[0].User)
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
