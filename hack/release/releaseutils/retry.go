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

package releaseutils

import (
	"context"
	"errors"
	"log"
	"time"
)

// Retry policy for registry uploads. GHCR occasionally answers "not found"
// right after an upload to a freshly created package; the second attempt
// succeeds.
const (
	RegistryAttempts   = 3
	RegistryRetryDelay = 10 * time.Second
)

// permanentError is an error Retry does not retry.
type permanentError struct {
	err error
}

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent marks an error as not worth retrying.
func Permanent(err error) error {
	return permanentError{err: err}
}

// Retry runs fn up to attempts times, pausing delay between failures.
// An error marked Permanent is returned at once.
func Retry(ctx context.Context, attempts int, delay time.Duration, fn func() error) error {
	var err error
	for i := 1; i <= attempts; i++ {
		err = fn()
		if err == nil || i == attempts || errors.As(err, &permanentError{}) {
			return err
		}
		log.Printf("attempt %d/%d failed: %v; retrying in %s", i, attempts, err, delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return err
}
