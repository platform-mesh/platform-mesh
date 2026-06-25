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
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStart(t *testing.T) {
	err := Start(context.Background(), "", "", "", "", "")
	assert.NoError(t, err)
}

func TestCaptureError(t *testing.T) {
	assert.NotPanics(t, func() {
		err := fmt.Errorf("test error")
		CaptureError(err, nil)
	})
}

func TestCaptureErrorNil(t *testing.T) {
	assert.NotPanics(t, func() {
		CaptureError(nil, nil)
	})
}

func TestCaptureSentryError(t *testing.T) {
	assert.NotPanics(t, func() {
		err := SentryError(fmt.Errorf("test error"))
		CaptureSentryError(err, nil)
	})
}

func TestWrap_NoPanic(t *testing.T) {
	called := false

	wrapped := Wrap(func() {
		called = true
	}, nil)

	assert.NotPanics(t, func() {
		wrapped()
	})

	assert.True(t, called, "wrapped function should be executed")
}

func TestWrap_PanicIsRecovered(t *testing.T) {
	wrapped := Wrap(func() {
		panic("nil pointer exception")
	}, Tags{"component": "test"})

	assert.NotPanics(t, func() {
		wrapped()
	})
}
