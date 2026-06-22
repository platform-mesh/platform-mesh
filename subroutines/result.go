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

package subroutines

import "time"

type action int

const (
	actionContinue action = iota
	actionPending
	actionStopWithRequeue
	actionStop
	actionSkipAll
	actionSkip
)

// Result encodes the outcome of a subroutine invocation.
// The zero value represents a successful continue with no requeue.
type Result struct {
	action  action
	requeue time.Duration
	message string
	ready   bool
}

// OK returns a Result that continues the chain with no requeue.
func OK() Result {
	return Result{}
}

// OKWithRequeue returns a Result that continues the chain and requeues after d.
func OKWithRequeue(d time.Duration) Result {
	return Result{action: actionContinue, requeue: d}
}

// Pending returns a Result that continues the chain, sets the condition to Unknown,
// and requeues after d. Use Pending when a subroutine is waiting on an external condition.
// Note: the lifecycle engine picks the shortest requeue across all subroutines, so a
// later subroutine returning OKWithRequeue with a shorter duration will take precedence.
// Panics if d <= 0.
func Pending(d time.Duration, msg string) Result {
	if d <= 0 {
		panic("subroutines: Pending requires a positive requeue duration")
	}
	return Result{action: actionPending, requeue: d, message: msg}
}

// StopWithRequeue returns a Result that stops the chain and requeues after d.
func StopWithRequeue(d time.Duration, msg string) Result {
	return Result{action: actionStopWithRequeue, requeue: d, message: msg}
}

// Stop returns a Result that stops the chain with no explicit requeue.
func Stop(msg string) Result {
	return Result{action: actionStop, message: msg}
}

// SkipAll halts the subroutine chain and marks remaining subroutines as Skipped.
// The ready flag controls the aggregate Ready condition: true sets it to True,
// false sets it to False.
func SkipAll(ready bool, msg string) Result {
	return Result{action: actionSkipAll, message: msg, ready: ready}
}

// Skip returns a Result that continues the chain but marks the condition as True/Skipped.
// Use Skip when a subroutine's preconditions are not met and it should be bypassed
// without interrupting the remaining chain.
func Skip(msg string) Result {
	return Result{action: actionSkip, message: msg}
}

// IsContinue returns true if the result is OK or OKWithRequeue.
// Note: Pending also continues the chain but returns false here — use IsPending
// to check for that case separately.
func (r Result) IsContinue() bool {
	return r.action == actionContinue
}

// IsPending returns true if the result is Pending.
func (r Result) IsPending() bool {
	return r.action == actionPending
}

// IsStopWithRequeue returns true if the result stops the chain with a requeue.
func (r Result) IsStopWithRequeue() bool {
	return r.action == actionStopWithRequeue
}

// IsStop returns true if the result stops the chain with no requeue.
func (r Result) IsStop() bool {
	return r.action == actionStop
}

// IsSkipAll returns true if the result halts the chain and marks remaining subroutines as Skipped.
func (r Result) IsSkipAll() bool {
	return r.action == actionSkipAll
}

// IsSkip returns true if the result continues the chain with a True/Skipped condition.
func (r Result) IsSkip() bool {
	return r.action == actionSkip
}

// Ready returns the ready flag, which controls the aggregate Ready condition for SkipAll results.
func (r Result) Ready() bool {
	return r.ready
}

// Requeue returns the requeue duration.
func (r Result) Requeue() time.Duration {
	return r.requeue
}

// Message returns the result message.
func (r Result) Message() string {
	return r.message
}
