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

package testSupport

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

func CreateFakeClient(t *testing.T, objects ...ctrlruntimeclient.Object) ctrlruntimeclient.WithWatch {
	builder := fake.NewClientBuilder()
	s := runtime.NewScheme()
	sBuilder := scheme.Builder{GroupVersion: schema.GroupVersion{Group: "test.platform-mesh.io", Version: "v1alpha1"}}
	sBuilder.Register(&TestApiObject{})
	for _, obj := range objects {
		sBuilder.Register(obj)
		builder.WithStatusSubresource(obj)
	}
	err := sBuilder.AddToScheme(s)
	assert.NoError(t, err)
	builder.WithScheme(s)
	builder.WithObjects(objects...)
	return builder.Build()
}
