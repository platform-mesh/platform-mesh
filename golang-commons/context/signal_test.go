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
	"errors"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNotifyContextSIGINT(t *testing.T) {
	ctx, _ := NotifyShutdownContext(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err := syscall.Kill(syscall.Getpid(), syscall.SIGINT)
		assert.NoError(t, err)
		wg.Done()
	}()

	wg.Wait()
	<-ctx.Done()
	err := ctx.Err()
	assert.NotNil(t, err)
	assert.True(t, errors.Is(err, context.Canceled))

	cause := context.Cause(ctx)
	assert.NotNil(t, cause)
	assert.True(t, errors.Is(cause, ErrShutdown))
}

func TestNotifyContextSIGTERM(t *testing.T) {
	ctx, _ := NotifyShutdownContext(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
		assert.NoError(t, err)
		wg.Done()
	}()

	wg.Wait()
	<-ctx.Done()
	err := ctx.Err()
	assert.NotNil(t, err)
	assert.True(t, errors.Is(err, context.Canceled))

	cause := context.Cause(ctx)
	assert.NotNil(t, cause)
	assert.True(t, errors.Is(cause, ErrShutdown))
}
