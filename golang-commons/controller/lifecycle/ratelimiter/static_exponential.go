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

package ratelimiter

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock"
)

type StaticThenExponentialRateLimiter[T comparable] struct {
	failuresLock   sync.RWMutex
	staticAttempts map[T]time.Time

	staticDelay  time.Duration
	staticWindow time.Duration

	exponential workqueue.TypedRateLimiter[T]
	clock       clock.PassiveClock
}

// NewStaticThenExponentialRateLimiter returns a limiter that tracks backoff per key.
// Pointer keys are rejected as they should not be used in controller context.
//
// Use the reconcile request as the key, or NewObjectRateLimiter for API objects.
func NewStaticThenExponentialRateLimiter[T comparable](cfg Config) (*StaticThenExponentialRateLimiter[T], error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if err := validateKey[T](); err != nil {
		return nil, err
	}
	clk := cfg.clock
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &StaticThenExponentialRateLimiter[T]{
		staticDelay:  cfg.StaticRequeueDelay,
		staticWindow: cfg.StaticWindow,
		exponential: workqueue.NewTypedItemExponentialFailureRateLimiter[T](
			cfg.ExponentialInitialBackoff,
			cfg.ExponentialMaxBackoff,
		),
		staticAttempts: make(map[T]time.Time),
		clock:          clk,
	}, nil
}

func (r *StaticThenExponentialRateLimiter[T]) When(item T) time.Duration {
	now := r.clock.Now()

	r.failuresLock.RLock()
	first, exists := r.staticAttempts[item]
	r.failuresLock.RUnlock()
	if !exists {
		r.failuresLock.Lock()
		r.staticAttempts[item] = now
		r.failuresLock.Unlock()
		return r.staticDelay
	}

	timeSinceFirst := now.Sub(first)
	if timeSinceFirst <= r.staticWindow {
		return r.staticDelay
	}

	return r.exponential.When(item)
}

func (r *StaticThenExponentialRateLimiter[T]) Forget(item T) {
	r.failuresLock.Lock()
	defer r.failuresLock.Unlock()

	delete(r.staticAttempts, item)
	r.exponential.Forget(item)
}

func (r *StaticThenExponentialRateLimiter[T]) NumRequeues(item T) int {
	return r.exponential.NumRequeues(item)
}

func validateKey[T comparable]() error {
	t := reflect.TypeFor[T]()
	if !comparesByValue(t) {
		return fmt.Errorf("key type %s compares by identity, use a value key such as the reconcile request or NewObjectRateLimiter", t)
	}
	return nil
}

// comparesByValue reports whether t compares by value and not by identity.
// Interfaces fail the check as e.g. client.Object can hold a pointer.
func comparesByValue(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.UnsafePointer, reflect.Interface, reflect.Chan:
		return false
	case reflect.Array:
		return comparesByValue(t.Elem())
	case reflect.Struct:
		for i := range t.NumField() {
			if !comparesByValue(t.Field(i).Type) {
				return false
			}
		}
	}
	return true
}
