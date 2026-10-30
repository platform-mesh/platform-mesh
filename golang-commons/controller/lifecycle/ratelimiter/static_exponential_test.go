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
	"unsafe"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	clocktesting "k8s.io/utils/clock/testing"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

func TestStaticThenExponentialRateLimiter_Forget(t *testing.T) {
	fakeClock := clocktesting.NewFakeClock(time.Now())
	cfg := NewConfig(
		WithRequeueDelay(1*time.Second),
		WithStaticWindow(5*time.Second),
		WithExponentialInitialBackoff(2*time.Second),
		WithExponentialMaxBackoff(1*time.Minute),
		WithClock(fakeClock),
	)
	limiter, err := NewStaticThenExponentialRateLimiter[reconcile.Request](cfg)
	require.NoError(t, err)

	item := reconcile.Request{NamespacedName: types.NamespacedName{Name: "name", Namespace: "namespace"}}
	require.Equal(t, cfg.StaticRequeueDelay, limiter.When(item))
	fakeClock.Step(10 * time.Second)
	require.Equal(t, cfg.ExponentialInitialBackoff, limiter.When(item), "past the static window")

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
func TestStaticThenExponentialRateLimiter_KeyTypes(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		tests := map[string]func() error{
			"reconcile request":    newLimiterFor[reconcile.Request],
			"multicluster request": newLimiterFor[mcreconcile.Request],
			"object identity key":  newLimiterFor[objectKey],
			"namespaced name":      newLimiterFor[types.NamespacedName],
			"string":               newLimiterFor[string],
		}
		for name, construct := range tests {
			t.Run(name, func(t *testing.T) {
				err := construct()

				require.NoError(t, err)
			})
		}
	})

	t.Run("rejects", func(t *testing.T) {
		tests := map[string]func() error{
			"pointer to an API object":       newLimiterFor[*corev1.ConfigMap],
			"client.Object interface":        newLimiterFor[ctrlruntimeclient.Object],
			"struct holding a pointer field": newLimiterFor[keyWithPointer],
			"array of pointers":              newLimiterFor[[1]*corev1.ConfigMap],
			"channel":                        newLimiterFor[chan struct{}],
			"unsafe pointer":                 newLimiterFor[unsafe.Pointer],
		}
		for name, construct := range tests {
			t.Run(name, func(t *testing.T) {
				err := construct()

				require.ErrorContains(t, err, "compares by identity")
			})
		}
	})
}

func newLimiterFor[T comparable]() error {
	_, err := NewStaticThenExponentialRateLimiter[T](NewConfig())
	return err
}

type keyWithPointer struct {
	Name string
	Obj  *corev1.ConfigMap
}
