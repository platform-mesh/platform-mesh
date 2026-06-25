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

package context

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testlogger "go.platform-mesh.io/golang-commons/logger/testlogger"
)

func TestRecover(t *testing.T) {
	t.Parallel()
	t.Run("should recover from panic and log", func(t *testing.T) {
		log := testlogger.New()
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer Recover(log.Logger)
			panic("test panic")
		}()
		wg.Wait()

		logMessages, err := log.GetLogMessages()
		assert.NoError(t, err)
		require.Len(t, logMessages, 1)
		assert.Equal(t, "recovered panic", logMessages[0].Message)
	})

	t.Run("should recover from panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			defer Recover(nil)
			panic("test panic")
		})
	})
}
