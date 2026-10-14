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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/kcp-dev/logicalcluster/v3"
)

var _ workqueue.TypedRateLimiter[*corev1.ConfigMap] = &ObjectRateLimiter[*corev1.ConfigMap]{}

func TestObjectRateLimiter(t *testing.T) {
	cfg := NewConfig(
		WithRequeueDelay(1*time.Second),
		WithStaticWindow(5*time.Second),
		WithExponentialInitialBackoff(2*time.Second),
		WithExponentialMaxBackoff(1*time.Minute),
	)
	t.Run("two instances of the same object", func(t *testing.T) {
		limiter, clock := setup(t, cfg)

		firstWait := limiter.When(configMapInCluster("root:provider", "default", "blap"))
		require.Equal(t, cfg.StaticRequeueDelay, firstWait)

		clock.Step(cfg.StaticWindow + time.Second)
		nextWait := limiter.When(configMapInCluster("root:provider", "default", "blap"))
		assert.Equal(t, cfg.ExponentialInitialBackoff, nextWait, "expected backoff in exponential window")
	})

	t.Run("same name in another cluster", func(t *testing.T) {
		limiter, clock := setup(t, cfg)

		firstWait := limiter.When(configMapInCluster("root:org-a", "default", "settings"))
		require.Equal(t, cfg.StaticRequeueDelay, firstWait)

		clock.Step(cfg.StaticWindow + time.Second)
		nextWait := limiter.When(configMapInCluster("root:org-a", "default", "settings"))
		require.Equal(t, cfg.ExponentialInitialBackoff, nextWait, "wait should be in the exponential window")

		otherClusterWait := limiter.When(configMapInCluster("root:org-b", "default", "settings"))
		assert.Equal(t, cfg.StaticRequeueDelay, otherClusterWait, "object from separate cluster should have its own backoff")
	})
}

func setup(t *testing.T, cfg Config) (*ObjectRateLimiter[*corev1.ConfigMap], *clocktesting.FakeClock) {
	t.Helper()
	clk := clocktesting.NewFakeClock(time.Now())
	WithClock(clk)(&cfg)
	limiter, err := NewObjectRateLimiter[*corev1.ConfigMap](cfg)
	require.NoError(t, err)
	return limiter, clk
}

func configMapInCluster(cluster logicalcluster.Name, namespace, name string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{logicalcluster.AnnotationKey: cluster.String()},
		},
	}
}
