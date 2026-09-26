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

package util

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureNamespace_createsDefault(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	require.NoError(t, EnsureNamespace(context.Background(), cl, "default"))
	require.NoError(t, EnsureNamespace(context.Background(), cl, "default"))

	ns := &corev1.Namespace{}
	require.NoError(t, cl.Get(context.Background(), ctrlruntimeclient.ObjectKey{Name: "default"}, ns))
	assert.Equal(t, "default", ns.Name)
}
