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
	"runtime/debug"
	"time"

	"github.com/getsentry/sentry-go"

	"go.platform-mesh.io/golang-commons/logger"
)

// Recover can be used as deferred function to catch panics
// This function is used in the context of context creation. Its contained in the context package to avoid circular dependencies with sentry package
func Recover(log *logger.Logger) {
	if log == nil {
		log = logger.StdLogger
	}

	if err := recover(); err != nil {
		log.Error().Interface("panic", err).Interface("stack", debug.Stack()).Msg("recovered panic")
		sentry.CurrentHub().Recover(err)
		sentry.Flush(time.Second * 5)
	}
}
