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

package apis

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConstants(t *testing.T) {
	t.Run("categories_extension_key", func(t *testing.T) {
		assert.Equal(t, "x-kubernetes-categories", CategoriesExtensionKey)
		assert.NotEmpty(t, CategoriesExtensionKey)
	})

	t.Run("gvk_extension_key", func(t *testing.T) {
		assert.Equal(t, "x-kubernetes-group-version-kind", GVKExtensionKey)
		assert.NotEmpty(t, GVKExtensionKey)
	})

	t.Run("scope_extension_key", func(t *testing.T) {
		assert.Equal(t, "x-kubernetes-scope", ScopeExtensionKey)
		assert.NotEmpty(t, ScopeExtensionKey)
	})
}

func TestConstantsFormat(t *testing.T) {
	constants := []string{
		CategoriesExtensionKey,
		GVKExtensionKey,
		ScopeExtensionKey,
	}

	for _, constant := range constants {
		assert.True(t, strings.HasPrefix(constant, "x-kubernetes-"))
		assert.NotContains(t, constant, " ")
	}
}
