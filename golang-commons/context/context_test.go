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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/golang-commons/logger"
)

type TestConfig struct {
	Key string
}

func TestStartContext(t *testing.T) {
	t.Parallel()

	log, err := logger.New(logger.DefaultConfig())
	assert.NoError(t, err)
	cfg := TestConfig{Key: "value"}

	ctx, cancel, shutdown := StartContext(log, cfg, 3*time.Second)
	defer shutdown()

	assert.NotNil(t, ctx)
	assert.NotNil(t, cancel)
}

func TestDefaultTimeout(t *testing.T) {
	t.Parallel()

	timeout := TimeoutFromContext(nil) //nolint:all
	assert.Equal(t, DefaultShutdownTimeout, timeout)

	timeoutCtx := TimeoutFromContext(context.Background())
	assert.Equal(t, DefaultShutdownTimeout, timeoutCtx)
}

func TestTimeoutFromContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = context.WithValue(ctx, ShutdownTimeoutKey{}, 5*time.Second)

	timeout := TimeoutFromContext(ctx)
	assert.Equal(t, 5*time.Second, timeout)
}
