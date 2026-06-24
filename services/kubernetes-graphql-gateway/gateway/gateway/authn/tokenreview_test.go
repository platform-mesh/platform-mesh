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

package authn

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestNoopValidatorAcceptsAnyToken(t *testing.T) {
	var v Validator = NoopValidator{}
	for _, tok := range []string{"", "bogus", "header.payload.sig"} {
		ok, err := v.Validate(context.Background(), tok)
		assert.NoError(t, err)
		assert.True(t, ok, "NoopValidator must accept token %q", tok)
	}
}

func fakeClientset(authenticated bool, calls *atomic.Int32, returnErr error) *fake.Clientset {
	cs := fake.NewClientset()
	cs.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		calls.Add(1)
		if returnErr != nil {
			return true, nil, returnErr
		}
		tr := action.(k8stesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		tr.Status.Authenticated = authenticated
		return true, tr, nil
	})
	return cs
}

func TestValidToken(t *testing.T) {
	var calls atomic.Int32
	v := NewTokenReviewValidatorFromClientset(fakeClientset(true, &calls, nil), 5*time.Minute)

	ok, err := v.Validate(t.Context(), "valid-token")

	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int32(1), calls.Load())
}

func TestInvalidToken(t *testing.T) {
	var calls atomic.Int32
	v := NewTokenReviewValidatorFromClientset(fakeClientset(false, &calls, nil), 5*time.Minute)

	ok, err := v.Validate(t.Context(), "invalid-token")

	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, int32(1), calls.Load())
}

func TestAPIError(t *testing.T) {
	var calls atomic.Int32
	v := NewTokenReviewValidatorFromClientset(fakeClientset(false, &calls, fmt.Errorf("connection refused")), 5*time.Minute)

	ok, err := v.Validate(t.Context(), "some-token")

	assert.Error(t, err)
	assert.False(t, ok)
	assert.Equal(t, int32(1), calls.Load())
}

func TestCacheHit(t *testing.T) {
	var calls atomic.Int32
	v := NewTokenReviewValidatorFromClientset(fakeClientset(true, &calls, nil), 5*time.Minute)

	ok, err := v.Validate(t.Context(), "cached-token")
	assert.NoError(t, err)
	assert.True(t, ok)

	ok, err = v.Validate(t.Context(), "cached-token")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int32(1), calls.Load(), "second call should use cache")
}

func TestCacheExpiry(t *testing.T) {
	var calls atomic.Int32
	v := NewTokenReviewValidatorFromClientset(fakeClientset(true, &calls, nil), 50*time.Millisecond)

	_, _ = v.Validate(t.Context(), "expiring-token")
	assert.Equal(t, int32(1), calls.Load())

	_, _ = v.Validate(t.Context(), "expiring-token")
	assert.Equal(t, int32(1), calls.Load(), "should use cache before expiry")

	time.Sleep(100 * time.Millisecond)

	_, _ = v.Validate(t.Context(), "expiring-token")
	assert.Equal(t, int32(2), calls.Load(), "should call API after expiry")
}

func TestCacheStoresInvalidResult(t *testing.T) {
	var calls atomic.Int32
	v := NewTokenReviewValidatorFromClientset(fakeClientset(false, &calls, nil), 5*time.Minute)

	ok, _ := v.Validate(t.Context(), "bad-token")
	assert.False(t, ok)

	ok, _ = v.Validate(t.Context(), "bad-token")
	assert.False(t, ok)
	assert.Equal(t, int32(1), calls.Load(), "invalid result should also be cached")
}

func TestAPIErrorNotCached(t *testing.T) {
	var callIdx atomic.Int32
	cs := fake.NewClientset()
	cs.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		idx := callIdx.Add(1)
		if idx == 1 {
			return true, nil, fmt.Errorf("transient error")
		}
		tr := action.(k8stesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		tr.Status.Authenticated = true
		return true, tr, nil
	})
	v := NewTokenReviewValidatorFromClientset(cs, 5*time.Minute)

	ok, err := v.Validate(t.Context(), "retry-token")
	assert.Error(t, err)
	assert.False(t, ok)

	ok, err = v.Validate(t.Context(), "retry-token")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int32(2), callIdx.Load())
}

func TestStartStopsOnCancel(t *testing.T) {
	var calls atomic.Int32
	v := NewTokenReviewValidatorFromClientset(fakeClientset(true, &calls, nil), 50*time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		v.Start(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not exit after context cancellation")
	}
}

func TestCacheDisabledWhenTTLZero(t *testing.T) {
	var calls atomic.Int32
	v := NewTokenReviewValidatorFromClientset(fakeClientset(true, &calls, nil), 0)

	_, _ = v.Validate(t.Context(), "same-token")
	_, _ = v.Validate(t.Context(), "same-token")
	assert.Equal(t, int32(2), calls.Load(), "every call should hit the API when caching is disabled")
}

func TestCacheTTLCappedAtTokenExpiry(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Second)),
	})
	shortLivedToken, err := token.SignedString([]byte("test-secret"))
	assert.NoError(t, err)

	var calls atomic.Int32
	v := NewTokenReviewValidatorFromClientset(fakeClientset(true, &calls, nil), 5*time.Minute)

	_, _ = v.Validate(t.Context(), shortLivedToken)
	assert.Equal(t, int32(1), calls.Load())

	_, _ = v.Validate(t.Context(), shortLivedToken)
	assert.Equal(t, int32(1), calls.Load(), "should use cache before token expiry")

	time.Sleep(1500 * time.Millisecond)

	_, _ = v.Validate(t.Context(), shortLivedToken)
	assert.Equal(t, int32(2), calls.Load(), "should call API after token expired")
}

func TestConcurrentValidation(t *testing.T) {
	var calls atomic.Int32
	v := NewTokenReviewValidatorFromClientset(fakeClientset(true, &calls, nil), 5*time.Minute)

	const goroutines = 20
	errCh := make(chan error, goroutines)

	for range goroutines {
		go func() {
			ok, err := v.Validate(t.Context(), "concurrent-token")
			if err != nil {
				errCh <- err
				return
			}
			if !ok {
				errCh <- fmt.Errorf("expected authenticated=true")
				return
			}
			errCh <- nil
		}()
	}

	for range goroutines {
		assert.NoError(t, <-errCh)
	}

	// singleflight deduplicates concurrent in-flight calls for the same key,
	// so we expect exactly 1 API call (all goroutines share the result).
	assert.Equal(t, int32(1), calls.Load(), "singleflight should deduplicate concurrent calls")
}
