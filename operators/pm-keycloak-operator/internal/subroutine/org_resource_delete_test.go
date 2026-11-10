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

package subroutine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeleteOrgResource_NotFound(t *testing.T) {
	t.Parallel()

	cl := newOrgResourceFakeClient(t)
	pending, err := deleteOrgResource(context.Background(), cl, &pmcorev1alpha1.Store{}, "missing")

	require.NoError(t, err)
	assert.False(t, pending)
}

func TestDeleteOrgResource_IssuesDelete(t *testing.T) {
	t.Parallel()

	cl := newOrgResourceFakeClient(t, orgStore("acme", "core.platform-mesh.io/fga-store"))
	pending, err := deleteOrgResource(context.Background(), cl, &pmcorev1alpha1.Store{}, "acme")

	require.NoError(t, err)
	assert.True(t, pending)

	var store pmcorev1alpha1.Store
	requireDeleting(t, cl, &store, "acme")
}

func TestDeleteOrgResource_PendingWhileDeleting(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	store := orgStore("acme", "core.platform-mesh.io/fga-store")
	store.DeletionTimestamp = &now
	cl := newOrgResourceFakeClient(t, store)

	pending, err := deleteOrgResource(context.Background(), cl, &pmcorev1alpha1.Store{}, "acme")

	require.NoError(t, err)
	assert.True(t, pending)
}
