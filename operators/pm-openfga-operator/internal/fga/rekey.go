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
	"slices"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
)

// StaleAccountTupleFilter returns a filter matching tuples that reference
// accountName under the account object type objectType with a cluster id other
// than currentClusterID, in either their Object or their User key.
func StaleAccountTupleFilter(objectType, accountName, currentClusterID string) TupleFilter {
	return func(t pmcorev1alpha1.Tuple) bool {
		return len(StaleClusterIDs(t, objectType, accountName, currentClusterID)) > 0
	}
}

// StaleClusterIDs returns the distinct cluster ids other than currentClusterID
// under which the tuple's Object or User key references accountName with the
// account object type objectType. Keys of any other object type are ignored
// even when they share the raw key shape and the account's name (see
// ParseAccountKey). A tuple built from account keys references exactly one
// cluster id; more than one returned id means the tuple is ambiguous and must
// not be re-keyed blindly.
func StaleClusterIDs(t pmcorev1alpha1.Tuple, objectType, accountName, currentClusterID string) []string {
	if objectType == "" || accountName == "" || currentClusterID == "" {
		return nil
	}

	var ids []string
	for _, s := range []string{t.Object, t.User} {
		k, ok := ParseAccountKey(s)
		if ok && k.ObjectType == objectType && k.Name == accountName && k.ClusterID != currentClusterID && !slices.Contains(ids, k.ClusterID) {
			ids = append(ids, k.ClusterID)
		}
	}
	return ids
}

// RekeyTuple returns a copy of t in which every Object and User key of the
// account object type objectType that carries oldClusterID is rewritten to
// carry newClusterID instead. Keys that do not parse as account keys, that
// carry another object type, or that carry a different cluster id, are left
// untouched.
func RekeyTuple(t pmcorev1alpha1.Tuple, objectType, oldClusterID, newClusterID string) pmcorev1alpha1.Tuple {
	t.Object = rekeyKey(t.Object, objectType, oldClusterID, newClusterID)
	t.User = rekeyKey(t.User, objectType, oldClusterID, newClusterID)
	return t
}

func rekeyKey(s, objectType, oldClusterID, newClusterID string) string {
	k, ok := ParseAccountKey(s)
	if !ok || k.ObjectType != objectType || k.ClusterID != oldClusterID {
		return s
	}
	k.ClusterID = newClusterID
	return k.String()
}
