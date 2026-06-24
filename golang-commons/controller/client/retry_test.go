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

package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/golang-commons/controller/testSupport"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRetry(t *testing.T) {
	o := &testSupport.TestApiObject{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
	}
	c := testSupport.CreateFakeClient(t, o)

	t.Run("Retry status update", func(t *testing.T) {
		// Arrange
		ctx := context.Background()

		// Act
		err := RetryStatusUpdate(ctx, func(object ctrlruntimeclient.Object) ctrlruntimeclient.Object {
			return object
		}, o, c)

		// Assert
		assert.NoError(t, err)
	})

	t.Run("Retry update", func(t *testing.T) {
		// Arrange
		ctx := context.Background()

		// Act
		err := RetryUpdate(ctx, func(object ctrlruntimeclient.Object) ctrlruntimeclient.Object {
			return object
		}, o, c)

		// Assert
		assert.NoError(t, err)
	})

	t.Run("Retry update and fail", func(t *testing.T) {
		// Arrange
		ctx := context.Background()
		newObject := &testSupport.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{Name: "test1", Namespace: "test1"},
		}

		// Act
		err := RetryUpdate(ctx, func(object ctrlruntimeclient.Object) ctrlruntimeclient.Object {
			return object
		}, newObject, c)

		// Assert
		assert.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err))
	})

	t.Run("Retry Status update and fail", func(t *testing.T) {
		// Arrange
		ctx := context.Background()
		newObject := &testSupport.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{Name: "test1", Namespace: "test1"},
		}

		// Act
		err := RetryStatusUpdate(ctx, func(object ctrlruntimeclient.Object) ctrlruntimeclient.Object {
			return object
		}, newObject, c)

		// Assert
		assert.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err))
	})
}
