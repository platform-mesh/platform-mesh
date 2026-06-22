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

package middleware

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/getsentry/sentry-go"
	"go.platform-mesh.io/golang-commons/logger"
)

// Recoverer implements a middleware that recover from panics, sends them to Sentry
// SentryRecoverer returns an http.Handler that wraps next and recovers from panics.
//
// If a panic occurs (except http.ErrAbortHandler) the middleware logs the panic and stack
// trace, reports the error to the current Sentry hub, flushes Sentry events (up to 5s),
// and responds with HTTP 500 Internal Server Error. The returned handler otherwise
// delegates to next.ServeHTTP.
func SentryRecoverer(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil && err != http.ErrAbortHandler {
				log := logger.LoadLoggerFromContext(r.Context())
				log.Error().Interface("panic", err).Interface("stack", debug.Stack()).Msg("recovered http panic")
				sentry.CurrentHub().Recover(err)
				sentry.Flush(time.Second * 5)

				w.WriteHeader(http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}
