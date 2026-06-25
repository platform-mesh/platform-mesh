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

package sentry

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSentryError(t *testing.T) {
	err := errors.New("test error")
	sentryError := SentryError(err)

	t.Run("New Sentry error", func(t *testing.T) {
		assert.True(t, IsSentryError(sentryError))

		// test if it still fulfills the error interface
		assert.Implements(t, (*error)(nil), sentryError)
	})

	t.Run("Sentry AddTags", func(t *testing.T) {
		newSentryError := SentryError(err)
		newSentryError.AddTag("key", "value")
		assert.Equal(t, Tags{"key": "value"}, newSentryError.GetTags())
	})

	t.Run("Sentry AddExtras", func(t *testing.T) {
		newSentryError := SentryError(err)
		newSentryError.AddExtra("key", "value")
		assert.Equal(t, Extras{"key": "value"}, newSentryError.GetExtras())
	})

	t.Run("Sentry GetReason", func(t *testing.T) {
		newSentryError := SentryError(err)
		sErr, ok := AsSentryError(newSentryError)
		assert.True(t, ok)
		assert.Equal(t, "test error", sErr.GetReason().Error())
	})

	t.Run("No Sentry error", func(t *testing.T) {
		assert.False(t, IsSentryError(err))

		// test if it still fulfills the error interface
		assert.Implements(t, (*error)(nil), sentryError)
	})

	t.Run("Wrapped Sentry error is a sentry error", func(t *testing.T) {
		// check if wrapped errors are still sentry.Error
		wrappedError := fmt.Errorf("another error: %w", sentryError)

		assert.True(t, IsSentryError(wrappedError))
	})

	t.Run("As Sentry error returns the sentry error", func(t *testing.T) {
		// check if wrapped errors are still sentry.Error
		wrappedError := fmt.Errorf("another error: %w", sentryError)

		originalError, ok := AsSentryError(wrappedError)
		assert.True(t, ok)
		assert.IsType(t, &Error{}, originalError)
	})

	t.Run("Unwrap the sentry error", func(t *testing.T) {
		unwrapped := errors.Unwrap(sentryError)
		assert.IsType(t, err, unwrapped)
	})

	t.Run("Should return nil if provided error is nil", func(t *testing.T) {
		err := SentryError(nil)

		assert.Nil(t, err)

		assert.False(t, IsSentryError(err))
	})
}
