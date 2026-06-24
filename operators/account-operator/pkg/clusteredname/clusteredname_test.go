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

package clusteredname_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/account-operator/pkg/clusteredname"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetClusteredName_NoClusterInContext(t *testing.T) {
	obj := &pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: "a",
		},
	}

	cn, ok := clusteredname.GetClusteredName(t.Context(), obj)

	require.False(t, ok)
	require.Equal(t, "a", cn.Name)
}
