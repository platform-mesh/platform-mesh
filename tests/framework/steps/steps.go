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

// Package steps runs a test as a list of steps that work like reconcilers:
// a step does one pass and returns an error while things are not where it
// wants them, and Run calls it again until it returns nil.
//
//	steps.Run(t, env,
//		steps.Step[*Env]{Name: "organization", Run: createOrganization},
//		steps.Step[*Env]{Name: "account", Run: createAccount},
//	)
//
// E is whatever the steps work on; it is handed to every call.
package steps

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go.platform-mesh.io/platform-mesh/tests/framework/wait"
)

const (
	// Timeout bounds one step, Interval is the pause between two calls of
	// it.
	Timeout  = 10 * time.Minute
	Interval = 2 * time.Second
)

// A Step is one thing a test does, or expects. Run does one pass and
// returns: nil when the step is done, an error when it is not there yet. So
// Run does not wait itself, and it has to be idempotent, because it is
// called again after an error. An error made with Terminal is not retried.
type Step[E any] struct {
	Name string
	Run  func(ctx context.Context, env E) error
}

// ErrTerminal is wrapped by the errors Terminal returns.
var ErrTerminal = errors.New("terminal")

// Terminal returns an error of a step that no later call of the step can
// fix: Run fails the test at once instead of calling the step again. It is
// for what is wrong, as opposed to not there yet.
func Terminal(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTerminal, fmt.Sprintf(format, args...))
}

// Run runs the steps against env.
//
// ORDER MATTERS: the steps run in the order given, and a step starts only
// once the one before it returned nil, so a step can rely on what the steps
// before it left behind. Run fails t, and runs no further step, when a step
// returns a terminal error or does not return nil within Timeout.
func Run[E any](t *testing.T, env E, steps ...Step[E]) {
	t.Helper()
	for _, step := range steps {
		start, calls := time.Now(), 0
		wait.Eventually(t, func() (bool, string) {
			calls++
			err := step.Run(t.Context(), env)
			if err == nil {
				return true, ""
			}
			if errors.Is(err, ErrTerminal) {
				t.Fatalf("step %q: %v", step.Name, err)
			}
			return false, err.Error()
		}, Timeout, Interval, "step %q", step.Name)
		t.Logf("step %q: done after %s (%d calls)", step.Name, time.Since(start).Round(time.Millisecond), calls)
	}
}
