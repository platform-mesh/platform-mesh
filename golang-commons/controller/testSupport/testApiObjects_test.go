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
)

func TestTestApiObject_DeepCopy(t *testing.T) {
	t.Run("DeepCopyObject", func(t *testing.T) {
		// Arrange
		instance := &TestApiObject{}

		// Act
		c := instance.DeepCopyObject()

		// Assert
		assert.Equal(t, instance, c)
	})

	t.Run("DeepCopyObject with nil", func(t *testing.T) {
		// Arrange
		var instance *TestApiObject

		// Act
		c := instance.DeepCopyObject()

		// Assert
		assert.Nil(t, c)
	})

	t.Run("DeepCopy", func(t *testing.T) {
		// Arrange
		var instance *TestApiObject

		// Act
		c := instance.DeepCopy()

		// Assert
		assert.Equal(t, instance, c)
	})
}
