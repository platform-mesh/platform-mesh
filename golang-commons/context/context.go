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

// Package context implements a safe way to create a context which will have a timeout after closing the context
//
// The basic usage is using the StartContext function with a direct defer call to shutdown afterwards.
// There is a Timeout set in the context so TimeoutFromContext will return the time this context will wait for subroutines to stop.
package context

import (
	"context"
	"time"

	"go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
)

// Can be used in StartContext to have a sane default timeout
const DefaultShutdownTimeout = 3 * time.Second

type ShutdownTimeoutKey struct{}

// Creates a new context and returns context, cancel and shutdown function. It should be directly followed by a defer call to shutdown.
func StartContext(log *logger.Logger, cfg any, timeout time.Duration) (context.Context, context.CancelCauseFunc, func()) {
	return StartWithContext(context.Background(), log, cfg, timeout)
}

func StartWithContext(ctx context.Context, log *logger.Logger, cfg any, timeout time.Duration) (context.Context, context.CancelCauseFunc, func()) {
	parentCtx := context.WithValue(ctx, ShutdownTimeoutKey{}, timeout)
	ctxWithCause, cancel := context.WithCancelCause(parentCtx)
	ctx, c := NotifyShutdownContext(ctxWithCause)
	ctx = logger.SetLoggerInContext(ctx, log)
	ctx = config.SetConfigInContext(ctx, cfg)

	return ctx, cancel, func() {
		Recover(log)
		c()
		<-time.After(timeout)
	}
}

// Returns the wrapped timeout inside the context and will return a default timeout of 3 seconds when the context does not exist or doesn't wrap a timeout.
func TimeoutFromContext(ctx context.Context) time.Duration {
	if ctx == nil {
		return DefaultShutdownTimeout
	}
	timeout, ok := ctx.Value(ShutdownTimeoutKey{}).(time.Duration)
	if !ok {
		return DefaultShutdownTimeout
	}
	return timeout
}
