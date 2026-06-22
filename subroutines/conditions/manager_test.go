package conditions

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.platform-mesh.io/subroutines"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// fakeConditionObject implements client.Object + ConditionAccessor.
type fakeConditionObject struct {
	metav1.ObjectMeta `json:"metadata"`
	conditions        []metav1.Condition
}

func (f *fakeConditionObject) GetObjectKind() schema.ObjectKind   { return schema.EmptyObjectKind }
func (f *fakeConditionObject) DeepCopyObject() runtime.Object     { cp := *f; return &cp }
func (f *fakeConditionObject) GetConditions() []metav1.Condition  { return f.conditions }
func (f *fakeConditionObject) SetConditions(c []metav1.Condition) { f.conditions = c }

func TestInitUnknownConditions(t *testing.T) {
	mgr := NewManager()
	obj := &fakeConditionObject{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	mgr.InitUnknownConditions(obj, []string{"sub1", "sub2"})

	require.Len(t, obj.conditions, 3) // sub1, sub2, Ready
	for _, name := range []string{"sub1", "sub2", ReadyCondition} {
		c := meta.FindStatusCondition(obj.conditions, name)
		require.NotNil(t, c, "condition %s should exist", name)
		assert.Equal(t, metav1.ConditionUnknown, c.Status)
		assert.Equal(t, ReasonUnknown, c.Reason)
		assert.Equal(t, int64(1), c.ObservedGeneration)
	}

	// Calling again should not overwrite existing conditions.
	obj.conditions[0].Status = metav1.ConditionTrue
	mgr.InitUnknownConditions(obj, []string{"sub1", "sub2"})
	assert.Equal(t, metav1.ConditionTrue, obj.conditions[0].Status)
}

func TestSetSubroutineCondition(t *testing.T) {
	tests := []struct {
		name       string
		result     subroutines.Result
		err        error
		isFinalize bool
		wantStatus metav1.ConditionStatus
		wantReason string
		wantType   string
	}{
		{
			name:       "OK result",
			result:     subroutines.OK(),
			wantStatus: metav1.ConditionTrue,
			wantReason: ReasonComplete,
			wantType:   "mysub",
		},
		{
			name:       "OKWithRequeue result",
			result:     subroutines.OKWithRequeue(5 * time.Second),
			wantStatus: metav1.ConditionTrue,
			wantReason: ReasonComplete,
			wantType:   "mysub",
		},
		{
			name:       "Pending result",
			result:     subroutines.Pending(10*time.Second, "waiting"),
			wantStatus: metav1.ConditionUnknown,
			wantReason: ReasonPending,
			wantType:   "mysub",
		},
		{
			name:       "StopWithRequeue result",
			result:     subroutines.StopWithRequeue(30*time.Second, "stopped"),
			wantStatus: metav1.ConditionFalse,
			wantReason: ReasonStopped,
			wantType:   "mysub",
		},
		{
			name:       "Stop result",
			result:     subroutines.Stop("halted"),
			wantStatus: metav1.ConditionFalse,
			wantReason: ReasonStopped,
			wantType:   "mysub",
		},
		{
			name:       "error",
			result:     subroutines.OK(),
			err:        errors.New("something broke"),
			wantStatus: metav1.ConditionFalse,
			wantReason: ReasonError,
			wantType:   "mysub",
		},
		{
			name:       "Skip result",
			result:     subroutines.Skip("not needed"),
			wantStatus: metav1.ConditionTrue,
			wantReason: ReasonSkipped,
			wantType:   "mysub",
		},
		{
			name:       "finalize suffix",
			result:     subroutines.OK(),
			isFinalize: true,
			wantStatus: metav1.ConditionTrue,
			wantReason: ReasonComplete,
			wantType:   "mysubFinalize",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager()
			obj := &fakeConditionObject{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
			mgr.SetSubroutineCondition(obj, "mysub", tt.result, tt.err, tt.isFinalize)

			c := meta.FindStatusCondition(obj.conditions, tt.wantType)
			require.NotNil(t, c)
			assert.Equal(t, tt.wantStatus, c.Status)
			assert.Equal(t, tt.wantReason, c.Reason)
		})
	}
}

func TestSetReadyCondition(t *testing.T) {
	tests := []struct {
		name       string
		reason     string
		wantStatus metav1.ConditionStatus
		wantReason string
	}{
		{
			name:       "all OK",
			reason:     ReasonComplete,
			wantStatus: metav1.ConditionTrue,
			wantReason: ReasonComplete,
		},
		{
			name:       "has errors",
			reason:     ReasonError,
			wantStatus: metav1.ConditionFalse,
			wantReason: ReasonError,
		},
		{
			name:       "has stopped",
			reason:     ReasonStopped,
			wantStatus: metav1.ConditionFalse,
			wantReason: ReasonStopped,
		},
		{
			name:       "has pending",
			reason:     ReasonPending,
			wantStatus: metav1.ConditionUnknown,
			wantReason: ReasonPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager()
			obj := &fakeConditionObject{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
			mgr.SetReadyCondition(obj, tt.reason)

			c := meta.FindStatusCondition(obj.conditions, ReadyCondition)
			require.NotNil(t, c)
			assert.Equal(t, tt.wantStatus, c.Status)
			assert.Equal(t, tt.wantReason, c.Reason)
		})
	}
}
