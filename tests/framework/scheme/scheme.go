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

// Package scheme builds the runtime schemes the integration tests use.
package scheme

import (
	"testing"

	"github.com/stretchr/testify/require"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	kcptopologyv1alpha1 "github.com/kcp-dev/sdk/apis/topology/v1alpha1"
)

// New returns a scheme with client-go's built-in types, apiextensions v1,
// kcp tenancy, core and topology v1alpha1 and kcp apis v1alpha1 and v1alpha2
// registered, plus the given adders (e.g. pmcorev1alpha1.AddToScheme).
func New(t testing.TB, adders ...func(*runtime.Scheme) error) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	builtin := []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		apiextensionsv1.AddToScheme,
		kcptenancyv1alpha1.AddToScheme,
		kcpcorev1alpha1.AddToScheme,
		kcptopologyv1alpha1.AddToScheme,
		kcpapisv1alpha1.AddToScheme,
		kcpapisv1alpha2.AddToScheme,
	}
	for _, add := range append(builtin, adders...) {
		require.NoError(t, add(s), "registering types in the scheme")
	}
	return s
}
