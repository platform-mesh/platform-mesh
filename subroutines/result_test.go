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

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestResult(t *testing.T) {
	tests := []struct {
		name            string
		result          Result
		wantContinue    bool
		wantPending     bool
		wantStopRequeue bool
		wantStop        bool
		wantSkip        bool
		wantRequeue     time.Duration
		wantMessage     string
	}{
		{
			name:         "zero value is continue",
			result:       Result{},
			wantContinue: true,
		},
		{
			name:         "OK",
			result:       OK(),
			wantContinue: true,
		},
		{
			name:         "OKWithRequeue",
			result:       OKWithRequeue(5 * time.Second),
			wantContinue: true,
			wantRequeue:  5 * time.Second,
		},
		{
			name:        "Pending",
			result:      Pending(10*time.Second, "waiting for dependency"),
			wantPending: true,
			wantRequeue: 10 * time.Second,
			wantMessage: "waiting for dependency",
		},
		{
			name:            "StopWithRequeue",
			result:          StopWithRequeue(30*time.Second, "rate limited"),
			wantStopRequeue: true,
			wantRequeue:     30 * time.Second,
			wantMessage:     "rate limited",
		},
		{
			name:        "Stop",
			result:      Stop("precondition failed"),
			wantStop:    true,
			wantMessage: "precondition failed",
		},
		{
			name:        "Skip",
			result:      Skip("not applicable"),
			wantSkip:    true,
			wantMessage: "not applicable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantContinue, tt.result.IsContinue())
			assert.Equal(t, tt.wantPending, tt.result.IsPending())
			assert.Equal(t, tt.wantStopRequeue, tt.result.IsStopWithRequeue())
			assert.Equal(t, tt.wantStop, tt.result.IsStop())
			assert.Equal(t, tt.wantSkip, tt.result.IsSkip())
			assert.Equal(t, tt.wantRequeue, tt.result.Requeue())
			assert.Equal(t, tt.wantMessage, tt.result.Message())
		})
	}
}

func TestPending_PanicsOnZeroDuration(t *testing.T) {
	assert.PanicsWithValue(t, "subroutines: Pending requires a positive requeue duration", func() {
		Pending(0, "bad")
	})
}
