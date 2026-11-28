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

package server

import (
	"errors"
	"net/http"

	"go.platform-mesh.io/golang-commons/logger"
)

// Public response body strings. These are the only error messages the
// handler sends to callers; detail is logged, not returned.
const (
	publicInvalidCredential = "InvalidCredential"
	publicForbidden         = "Forbidden"
	publicInternalError     = "InternalError"
)

// httpError carries both the HTTP status code and a split between what
// is safe to send to the (possibly unauthenticated) caller and what
// should stay in the server logs. writeError consumes both.
type httpError struct {
	code   int
	public string
	err    error
}

func (e *httpError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return e.public
}

func (e *httpError) Unwrap() error { return e.err }

// httpErr builds an httpError whose public body is `public` and whose
// internal detail is `err` (may be nil for pure-sentinel cases).
func httpErr(code int, public string, err error) *httpError {
	return &httpError{code: code, public: public, err: err}
}

func writeError(w http.ResponseWriter, log *logger.Logger, err error) {
	var httpErr *httpError
	if errors.As(err, &httpErr) {
		if httpErr.err != nil {
			log.Debug().
				Int("code", httpErr.code).
				Str("public", httpErr.public).
				Err(httpErr.err).
				Msg("handler rejected request")
		}
		http.Error(w, httpErr.public, httpErr.code)
		return
	}
	log.Error().Err(err).Msg("handler returned uncategorised error")
	http.Error(w, publicInternalError, http.StatusInternalServerError)
}

// logFromRequest returns the request-scoped logger populated by the
// shared middleware chain, falling back to the stdlib logger if the
// middleware is absent (e.g. in unit tests calling the handler directly).
func logFromRequest(r *http.Request) *logger.Logger {
	return logger.LoadLoggerFromContext(r.Context())
}
