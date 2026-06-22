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

package conditions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConditionsService(t *testing.T) {
	conditions := []metav1.Condition{}

	t.Run("New Conditions Service", func(t *testing.T) {
		// Arrange + Act
		setter := NewConditionsService("MyConditionType")

		// Assert
		assert.NotNil(t, setter)
	})

	t.Run("Set to True", func(t *testing.T) {
		// Arrange
		setter := NewConditionsService("MyConditionType")

		// Act
		setter.SetTrue(metav1.ObjectMeta{}, &conditions, "reason", "message")

		// Assert
		assert.Equal(t, len(conditions), 1)
		assert.Equal(t, conditions[0].Status, metav1.ConditionTrue)
		assert.Equal(t, conditions[0].Type, "MyConditionType")
		assert.Equal(t, conditions[0].ObservedGeneration, int64(0))
		assert.Equal(t, conditions[0].Reason, "reason")
		assert.Equal(t, conditions[0].Message, "message")
	})

	t.Run("Set to False", func(t *testing.T) {
		// Arrange
		setter := NewConditionsService("MyConditionType")

		// Act
		setter.SetFalse(metav1.ObjectMeta{}, &conditions, "reason", "message")

		// Assert
		assert.Equal(t, len(conditions), 1)
		assert.Equal(t, conditions[0].Status, metav1.ConditionFalse)
		assert.Equal(t, conditions[0].Type, "MyConditionType")
		assert.Equal(t, conditions[0].ObservedGeneration, int64(0))
		assert.Equal(t, conditions[0].Reason, "reason")
		assert.Equal(t, conditions[0].Message, "message")
	})

	t.Run("GetValue, False", func(t *testing.T) {
		// Arrange
		setter := NewConditionsService("MyConditionType")

		// Act
		setter.SetFalse(metav1.ObjectMeta{}, &conditions, "reason", "message")
		val := setter.GetStatus(conditions)

		// Assert
		assert.Equal(t, val.Status, metav1.ConditionFalse)
	})

	t.Run("GetValue, nil", func(t *testing.T) {
		// Arrange
		setter := NewConditionsService("MyConditionType")

		// Act
		val := setter.GetStatus(nil)

		// Assert
		assert.Nil(t, val)
	})

	t.Run("GetValue, nil", func(t *testing.T) {
		// Arrange
		setter := NewConditionsService("MyConditionType")

		// Act
		val := setter.GetStatus([]metav1.Condition{})

		// Assert
		assert.Nil(t, val)
	})
	t.Run("GetValue, true", func(t *testing.T) {
		// Arrange
		setter := NewConditionsService("MyConditionType")

		// Act
		setter.SetTrue(metav1.ObjectMeta{}, &conditions, "reason", "message")
		val := setter.GetStatus(conditions)

		// Assert
		assert.Equal(t, val.Status, metav1.ConditionTrue)
	})

	t.Run("IsStatusTrue, true", func(t *testing.T) {
		// Arrange
		setter := NewConditionsService("MyConditionType")

		// Act
		setter.SetTrue(metav1.ObjectMeta{}, &conditions, "reason", "message")
		val := setter.IsStatusTrue(conditions)

		// Assert
		assert.True(t, val)
	})

	t.Run("IsStatusFalse, false", func(t *testing.T) {
		// Arrange
		setter := NewConditionsService("MyConditionType")

		// Act
		setter.SetFalse(metav1.ObjectMeta{}, &conditions, "reason", "message")
		val := setter.IsStatusTrue(conditions)

		// Assert
		assert.False(t, val)

		// Act
		val = setter.IsStatusTrue(nil)

		// Assert
		assert.False(t, val)

		// Act
		val = setter.IsStatusTrue([]metav1.Condition{})

		// Assert
		assert.False(t, val)
	})
}
