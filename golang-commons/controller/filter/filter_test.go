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

package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/golang-commons/controller/testSupport"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestFilter(t *testing.T) {
	predicate := DebugResourcesBehaviourPredicate("test")

	t.Run("Filter out test", func(t *testing.T) {
		// Arrange
		object := &testSupport.TestApiObject{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{DebugLabel: "test"}}}
		c := event.CreateEvent{Object: object}
		u := event.UpdateEvent{ObjectOld: object, ObjectNew: object}
		d := event.DeleteEvent{Object: object}
		g := event.GenericEvent{Object: object}

		// Act
		val := predicate.Create(c)
		val = predicate.Update(u) || val
		val = predicate.Delete(d) || val
		val = predicate.Generic(g) || val

		// Assert
		assert.True(t, val)
	})

	t.Run("Accept test", func(t *testing.T) {
		// Arrange
		object := &testSupport.TestApiObject{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}}}
		c := event.CreateEvent{Object: object}
		u := event.UpdateEvent{ObjectOld: object, ObjectNew: object}
		d := event.DeleteEvent{Object: object}
		g := event.GenericEvent{Object: object}

		// Act
		val := predicate.Create(c)
		val = predicate.Update(u) && val
		val = predicate.Delete(d) && val
		val = predicate.Generic(g) && val

		// Assert
		assert.False(t, val)
	})
}
