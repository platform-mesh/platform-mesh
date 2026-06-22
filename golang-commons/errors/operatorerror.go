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

package errors

type operatorError struct {
	err    error
	retry  bool
	sentry bool
}

func (e *operatorError) Err() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *operatorError) Retry() bool {
	return e != nil && e.err != nil && e.retry
}

func (e *operatorError) Sentry() bool {
	return e != nil && e.err != nil && e.sentry
}

type OperatorError interface {
	Err() error
	Retry() bool
	Sentry() bool
}

func NewOperatorError(err error, retry bool, sentry bool) OperatorError {
	return &operatorError{err: err, retry: retry, sentry: sentry}
}
