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
	"time"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/logicalcluster/v3"
)

type objectKey struct {
	cluster   logicalcluster.Name
	namespace string
	name      string
}

type ObjectRateLimiter[T ctrlruntimeclient.Object] struct {
	limiter *StaticThenExponentialRateLimiter[objectKey]
}

// NewObjectRateLimiter returns a rate limiter which uses the API object identity as key.
func NewObjectRateLimiter[T ctrlruntimeclient.Object](cfg Config) (*ObjectRateLimiter[T], error) {
	limiter, err := NewStaticThenExponentialRateLimiter[objectKey](cfg)
	if err != nil {
		return nil, err
	}
	return &ObjectRateLimiter[T]{
		limiter: limiter,
	}, nil
}

func (r *ObjectRateLimiter[T]) When(obj T) time.Duration {
	return r.limiter.When(keyOf(obj))
}

func (r *ObjectRateLimiter[T]) Forget(obj T) { r.limiter.Forget(keyOf(obj)) }

func (r *ObjectRateLimiter[T]) NumRequeues(obj T) int { return r.limiter.NumRequeues(keyOf(obj)) }

// keyOf returns the key for the Kubernetes API object.
func keyOf(obj ctrlruntimeclient.Object) objectKey {
	return objectKey{
		cluster:   logicalcluster.From(obj),
		namespace: obj.GetNamespace(),
		name:      obj.GetName(),
	}
}
