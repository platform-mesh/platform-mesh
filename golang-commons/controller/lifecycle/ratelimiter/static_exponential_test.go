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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	clocktesting "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestStaticThenExponentialRateLimiter_Forget(t *testing.T) {
	cfg := Config{
		StaticRequeueDelay:        1 * time.Second,
		StaticWindow:              5 * time.Second,
		ExponentialInitialBackoff: 2 * time.Second,
		ExponentialMaxBackoff:     1 * time.Minute,
	}
	limiter, err := NewStaticThenExponentialRateLimiter[reconcile.Request](cfg)
	require.NoError(t, err)
	fakeClock := clocktesting.NewFakeClock(time.Now())
	limiter.clock = fakeClock

	item := reconcile.Request{NamespacedName: types.NamespacedName{Name: "name", Namespace: "namespace"}}
	require.Equal(t, cfg.StaticRequeueDelay, limiter.When(item))
	fakeClock.Step(10 * time.Second)
	_ = limiter.When(item)

	limiter.Forget(item)
	require.Equal(t, 0, limiter.NumRequeues(item))
	delay := limiter.When(item)
	require.Equal(t, cfg.StaticRequeueDelay, delay)
}

func TestStaticThenExponentialRateLimiter_InvalidConfig(t *testing.T) {
	t.Run("negative static requeue delay", func(t *testing.T) {
		cfg := Config{
			StaticRequeueDelay:        -1 * time.Second,
			StaticWindow:              5 * time.Second,
			ExponentialInitialBackoff: 2 * time.Second,
			ExponentialMaxBackoff:     1 * time.Minute,
		}
		_, err := NewStaticThenExponentialRateLimiter[reconcile.Request](cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "static requeue delay shouldn't be negative")
	})

	t.Run("negative exponential initial backoff", func(t *testing.T) {
		cfg := Config{
			StaticRequeueDelay:        1 * time.Second,
			StaticWindow:              5 * time.Second,
			ExponentialInitialBackoff: -1 * time.Second,
			ExponentialMaxBackoff:     1 * time.Minute,
		}
		_, err := NewStaticThenExponentialRateLimiter[reconcile.Request](cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "initial exponential backoff shouldn't be negative")
	})

	t.Run("static requeue delay greater than exponential initial backoff", func(t *testing.T) {
		cfg := Config{
			StaticRequeueDelay:        5 * time.Second,
			StaticWindow:              10 * time.Second,
			ExponentialInitialBackoff: 2 * time.Second,
			ExponentialMaxBackoff:     1 * time.Minute,
		}
		_, err := NewStaticThenExponentialRateLimiter[reconcile.Request](cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "initial exponential backoff should be equal to or greater than the static requeue delay")
	})

	t.Run("static window less than static requeue delay", func(t *testing.T) {
		cfg := Config{
			StaticRequeueDelay:        5 * time.Second,
			StaticWindow:              2 * time.Second,
			ExponentialInitialBackoff: 5 * time.Second,
			ExponentialMaxBackoff:     1 * time.Minute,
		}
		_, err := NewStaticThenExponentialRateLimiter[reconcile.Request](cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "static window duration should be equal to or greater than the static requeue delay")
	})
}
