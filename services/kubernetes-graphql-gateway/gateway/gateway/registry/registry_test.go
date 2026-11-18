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

package registry_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/config"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/registry"
)

func TestRegistry_LoadFailure(t *testing.T) {
	r := registry.New(config.Gateway{})

	r.OnSchemaChanged(t.Context(), "cluster", []byte("not a schema"))

	_, exists := r.GetEndpoint("cluster")
	assert.False(t, exists)
	assert.True(t, r.LoadFailed("cluster"))
	assert.False(t, r.LoadFailed("other"))

	r.OnSchemaDeleted(t.Context(), "cluster")

	assert.False(t, r.LoadFailed("cluster"))
}
