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
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const orgTerminateTestOrg = "acme"

func newOrgResourceFakeClient(t *testing.T, objs ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}

func orgStore(name string, finalizers ...string) *pmcorev1alpha1.Store {
	return &pmcorev1alpha1.Store{ObjectMeta: metav1.ObjectMeta{Name: name, Finalizers: finalizers}}
}

func logicalClusterForOrg(name string) *kcpcorev1alpha1.LogicalCluster {
	return &kcpcorev1alpha1.LogicalCluster{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"kcp.io/path": "root:orgs:" + name,
			},
		},
	}
}

func requireDeleting(t *testing.T, cl ctrlruntimeclient.Client, obj ctrlruntimeclient.Object, name string) {
	t.Helper()

	require.NoError(t, cl.Get(context.Background(), ctrlruntimeclient.ObjectKey{Name: name}, obj))
	assert.NotNil(t, obj.GetDeletionTimestamp())
}
