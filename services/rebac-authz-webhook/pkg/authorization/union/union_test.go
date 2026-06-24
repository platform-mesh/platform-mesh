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

package union_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization/union"
)

type mockHandler struct {
	mock.Mock
}

func (m *mockHandler) Handle(ctx context.Context, req authorization.Request) authorization.Response {
	args := m.Called(ctx, req)
	return args.Get(0).(authorization.Response)
}

func TestUnion(t *testing.T) {
	t.Run("single allowed", func(t *testing.T) {
		m1 := &mockHandler{}
		m1.On("Handle", mock.Anything, mock.Anything).Return(authorization.Allowed()).Once()

		h := union.New(m1)
		res := h.Handle(t.Context(), authorization.Request{})

		assert.True(t, res.Status.Allowed)
		m1.AssertExpectations(t)
	})

	t.Run("single denied", func(t *testing.T) {
		m1 := &mockHandler{}
		m1.On("Handle", mock.Anything, mock.Anything).Return(authorization.Denied()).Once()

		h := union.New(m1)
		res := h.Handle(t.Context(), authorization.Request{})

		assert.True(t, res.Status.Denied)
		m1.AssertExpectations(t)
	})

	t.Run("no opinion then allowed stops early", func(t *testing.T) {
		mNo := &mockHandler{}
		mAllow := &mockHandler{}
		mSkip := &mockHandler{}

		mNo.On("Handle", mock.Anything, mock.Anything).Return(authorization.NoOpinion()).Once()
		mAllow.On("Handle", mock.Anything, mock.Anything).Return(authorization.Allowed()).Once()
		// mSkip should never be called
		h := union.New(mNo, mAllow, mSkip)

		res := h.Handle(t.Context(), authorization.Request{})

		assert.True(t, res.Status.Allowed)
		mNo.AssertNumberOfCalls(t, "Handle", 1)
		mAllow.AssertNumberOfCalls(t, "Handle", 1)
		mSkip.AssertNumberOfCalls(t, "Handle", 0)
	})

	t.Run("denied stops before later handlers", func(t *testing.T) {
		mDenied := &mockHandler{}
		mLater := &mockHandler{}

		mDenied.On("Handle", mock.Anything, mock.Anything).Return(authorization.Denied()).Once()
		h := union.New(mDenied, mLater)

		res := h.Handle(t.Context(), authorization.Request{})

		assert.True(t, res.Status.Denied)
		mDenied.AssertNumberOfCalls(t, "Handle", 1)
		mLater.AssertNumberOfCalls(t, "Handle", 0)
	})

	t.Run("abort stops chain without allow/deny", func(t *testing.T) {
		mNo := &mockHandler{}
		mAbort := &mockHandler{}
		mLater := &mockHandler{}

		mNo.On("Handle", mock.Anything, mock.Anything).Return(authorization.NoOpinion()).Once()
		mAbort.On("Handle", mock.Anything, mock.Anything).Return(authorization.Aborted()).Once()
		h := union.New(mNo, mAbort, mLater)

		res := h.Handle(t.Context(), authorization.Request{})

		assert.False(t, res.Status.Allowed)
		assert.False(t, res.Status.Denied)
		mNo.AssertNumberOfCalls(t, "Handle", 1)
		mAbort.AssertNumberOfCalls(t, "Handle", 1)
		mLater.AssertNumberOfCalls(t, "Handle", 0)
	})

	t.Run("all handlers NoOpinion returns implicit NoOpinion", func(t *testing.T) {
		m1 := &mockHandler{}
		m2 := &mockHandler{}

		m1.On("Handle", mock.Anything, mock.Anything).Return(authorization.NoOpinion()).Once()
		m2.On("Handle", mock.Anything, mock.Anything).Return(authorization.NoOpinion()).Once()
		h := union.New(m1, m2)

		res := h.Handle(t.Context(), authorization.Request{})

		assert.False(t, res.Status.Allowed)
		assert.False(t, res.Status.Denied)
		assert.False(t, res.Abort)
		assert.Equal(t, "NoOpinion", res.Status.Reason)
		m1.AssertNumberOfCalls(t, "Handle", 1)
		m2.AssertNumberOfCalls(t, "Handle", 1)
	})
}
