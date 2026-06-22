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

package errors

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestRetry(t *testing.T) {

	t.Run("Is retryable with an internal error", func(t *testing.T) {
		// Arrange
		e := k8sErrors.NewServiceUnavailable("na")

		// Act
		retriable, result := IsRetriable(e)

		// Assert
		assert.True(t, retriable)
		assert.Equal(t, time.Duration(0), result.RequeueAfter)
	})

	t.Run("Is retryable with a unknown error", func(t *testing.T) {
		// Arrange
		e := fmt.Errorf("oh nose")

		// Act
		retriable, result := IsRetriable(e)

		// Assert
		assert.False(t, retriable)
		assert.Equal(t, time.Duration(0), result.RequeueAfter)
	})

	t.Run("Is retryable with a clientDelay", func(t *testing.T) {
		// Arrange
		e := k8sErrors.NewTimeoutError("oh nose", 5)

		// Act
		retriable, result := IsRetriable(e)

		// Assert
		assert.True(t, retriable)
		assert.NotEqualf(t, time.Duration(0), result.RequeueAfter, "Expected requeueAfter to be set, but got %v", result.RequeueAfter)
	})
}
