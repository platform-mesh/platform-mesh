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

package sentry

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/getsentry/sentry-go"
)

type (
	Tags   map[string]string
	Extras map[string]any
)

const maxErrorDepth = 10

// Start initializes Sentry and flushes errors when the provides context is finished
func Start(ctx context.Context, dsn, env, region, name, tag string) error {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      fmt.Sprintf("%s-%s", env, region),
		Release:          fmt.Sprintf("%s:%s", name, tag),
		AttachStacktrace: true,
		TracesSampleRate: 1.0,
	})
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		sentry.Flush(5 * time.Second)
	}()

	return nil
}

// CaptureError sends an error to Sentry with provided tags and extras
func CaptureError(err error, tags Tags, extras ...Extras) {
	if err == nil || !ShouldBeProcessed(err) {
		return
	}

	sentry.WithScope(func(scope *sentry.Scope) {
		sentryErr, ok := AsSentryError(err)
		if ok {
			scope.SetTags(sentryErr.tags)
			scope.SetContext("extras", sentry.Context(sentryErr.extras))
		}

		scope.SetTags(tags)
		for _, extra := range extras {
			scope.SetContext("extras", sentry.Context(extra))
		}

		// set up a Sentry Event
		e := sentry.NewEvent()
		e.Level = sentry.LevelError
		e.Message = err.Error()
		e.Timestamp = time.Now()

		// iterate over all potentially wrapped errors
		for i := 0; i < maxErrorDepth && err != nil; i++ {
			// init exception and extract stacktrace
			se := sentry.Exception{
				Value:      err.Error(),
				Stacktrace: sentry.ExtractStacktrace(err),
			}

			// add the error type name only if it's not *sentry.Error
			if reflect.TypeOf(err) != reflect.TypeOf(&Error{}) {
				se.Type = reflect.TypeOf(err).String()
			} else {
				// set the error value as type to have better Sentry output
				se.Type = err.Error()
			}

			e.Exception = append(e.Exception, se)

			// follow the wrapped error chain
			switch previous := err.(type) {
			case interface{ Unwrap() error }:
				err = previous.Unwrap()
			case interface{ Cause() error }:
				err = previous.Cause()
			default:
				err = nil
			}
		}

		// if the most recent error doesn't come with a stacktrace, add it
		if e.Exception[0].Stacktrace == nil {
			e.Exception[0].Stacktrace = sentry.NewStacktrace()
		}

		// reverse exceptions so the most recent one is last
		for i, j := 0, len(e.Exception)-1; i < j; i, j = i+1, j-1 {
			e.Exception[i], e.Exception[j] = e.Exception[j], e.Exception[i]
		}

		sentry.CaptureEvent(e)
	})
}

// CaptureSentryError is a small wrapper that only captures Sentry errors
func CaptureSentryError(err error, tags Tags, extras ...Extras) {
	if IsSentryError(err) {
		CaptureError(err, tags, extras...)
	}
}

// Wrap returns a function wrapper that recovers from panics and sends them to Sentry.
//
// Usage:
//
//	// synchronous usage
//	wrapped := sentry.Wrap(func() { /* work */ }, sentry.Tags{"component":"job"})
//	wrapped()
//
//	// asynchronous usage
//	go sentry.Wrap(func() { /* work */ }, sentry.Tags{"component":"worker"})()
//
// Any panic inside the provided function will be captured and reported to Sentry
// with the tag panic=true alongside any provided tags and extras.
func Wrap(f func(), tags Tags, extras ...Extras) func() {
	return func() {
		defer func() {
			if r := recover(); r != nil {
				var err error
				switch v := r.(type) {
				case error:
					err = v
				default:
					err = fmt.Errorf("panic: %v", v)
				}

				// ensure we always tag this as a panic while preserving any provided tags
				merged := Tags{"panic": "true"}
				for k, v := range tags {
					merged[k] = v
				}

				CaptureError(err, merged, extras...)
			}
		}()

		f()
	}
}

// Go starts a goroutine that recovers from panics and sends them to Sentry.
func Go(f func(), tags Tags, extras ...Extras) {
	go Wrap(f, tags, extras...)()
}

// Add adds a new tag
func (t Tags) Add(key, value string) {
	t[key] = value
}

// Add adds a new extra data field
func (e Extras) Add(key string, value any) {
	e[key] = value
}
