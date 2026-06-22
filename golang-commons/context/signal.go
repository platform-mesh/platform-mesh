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
	"os"
	"os/signal"
	"syscall"
)

type signalCtx struct {
	context.Context

	cancel  context.CancelCauseFunc
	signals []os.Signal
	ch      chan os.Signal
}

func (c *signalCtx) stop() {
	c.cancel(nil)
	signal.Stop(c.ch)
}

var ErrShutdown = errors.New("shutdown")

// NotifyShutdownContext returns a copy of the parent context that is marked done
// (its Done channel is closed) when one of the expected signals arrives,
// when the returned stop function is called, or when the parent context's
// Done channel is closed, whichever happens first.
func NotifyShutdownContext(parent context.Context) (ctx context.Context, stop context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	signals := []os.Signal{syscall.SIGKILL, syscall.SIGTERM, syscall.SIGINT}
	c := &signalCtx{
		Context: ctx,
		cancel:  cancel,
		signals: signals,
	}
	c.ch = make(chan os.Signal, 1)
	signal.Notify(c.ch, c.signals...)
	if ctx.Err() == nil {
		go func() {
			select {
			case <-c.ch:
				c.cancel(ErrShutdown)
			case <-c.Done():
			}
		}()
	}
	return c, c.stop
}
