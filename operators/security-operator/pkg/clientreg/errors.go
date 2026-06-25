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

package clientreg

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

const maxErrorBodySize = 4096

type HTTPError struct {
	StatusCode int
	Body       string
	Operation  OperationType
}

func (e *HTTPError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("oidc %s failed: HTTP %d: %s", e.Operation, e.StatusCode, e.Body)
	}
	return fmt.Sprintf("oidc %s failed: HTTP %d", e.Operation, e.StatusCode)
}

func NewHTTPError(statusCode int, body string, operation OperationType) *HTTPError {
	return &HTTPError{
		StatusCode: statusCode,
		Body:       body,
		Operation:  operation,
	}
}

func IsHTTPError(err error) (*HTTPError, bool) {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr, true
	}
	return nil, false
}

func IsUnauthorized(err error) bool {
	httpErr, ok := IsHTTPError(err)
	return ok && httpErr.StatusCode == http.StatusUnauthorized
}

func IsNotFound(err error) bool {
	httpErr, ok := IsHTTPError(err)
	return ok && httpErr.StatusCode == http.StatusNotFound
}

var ErrNoTokenProvider = errors.New("oidc: token provider is required for this operation")
var ErrNoRegistrationURI = errors.New("oidc: registration client URI is required")

func newHTTPErrorFromResponse(resp *http.Response, operation OperationType) *HTTPError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
	return NewHTTPError(resp.StatusCode, string(body), operation)
}
