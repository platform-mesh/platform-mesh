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

package spread

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// fakeSpreadObject implements client.Object + SpreadReconcileStatus.
type fakeSpreadObject struct {
	metav1.ObjectMeta  `json:"metadata"`
	observedGeneration int64
	nextReconcileTime  metav1.Time
}

func (f *fakeSpreadObject) GetObjectKind() schema.ObjectKind   { return schema.EmptyObjectKind }
func (f *fakeSpreadObject) DeepCopyObject() runtime.Object     { cp := *f; return &cp }
func (f *fakeSpreadObject) GetObservedGeneration() int64       { return f.observedGeneration }
func (f *fakeSpreadObject) SetObservedGeneration(g int64)      { f.observedGeneration = g }
func (f *fakeSpreadObject) GetNextReconcileTime() metav1.Time  { return f.nextReconcileTime }
func (f *fakeSpreadObject) SetNextReconcileTime(t metav1.Time) { f.nextReconcileTime = t }

func newFakeSpreadObject() *fakeSpreadObject {
	return &fakeSpreadObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test",
			Namespace:  "default",
			Generation: 1,
			UID:        types.UID("test-uid"),
		},
		observedGeneration: 1,
	}
}

func TestReconcileRequired(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*fakeSpreadObject)
		want  bool
	}{
		{
			name: "generation changed",
			setup: func(obj *fakeSpreadObject) {
				obj.Generation = 2
				obj.observedGeneration = 1
				obj.nextReconcileTime = metav1.NewTime(time.Now().Add(1 * time.Hour))
			},
			want: true,
		},
		{
			name: "refresh label present",
			setup: func(obj *fakeSpreadObject) {
				obj.Labels = map[string]string{RefreshLabel: "true"}
				obj.nextReconcileTime = metav1.NewTime(time.Now().Add(1 * time.Hour))
			},
			want: true,
		},
		{
			name: "past next reconcile time",
			setup: func(obj *fakeSpreadObject) {
				obj.nextReconcileTime = metav1.NewTime(time.Now().Add(-1 * time.Hour))
			},
			want: true,
		},
		{
			name:  "zero next reconcile time",
			setup: func(obj *fakeSpreadObject) {},
			want:  true,
		},
		{
			name: "not yet due",
			setup: func(obj *fakeSpreadObject) {
				obj.nextReconcileTime = metav1.NewTime(time.Now().Add(1 * time.Hour))
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager()
			obj := newFakeSpreadObject()
			tt.setup(obj)
			assert.Equal(t, tt.want, mgr.ReconcileRequired(obj))
		})
	}
}

func TestRequeueDelay(t *testing.T) {
	mgr := NewManager()

	t.Run("zero time returns zero", func(t *testing.T) {
		obj := newFakeSpreadObject()
		assert.Equal(t, time.Duration(0), mgr.RequeueDelay(obj))
	})

	t.Run("past time returns zero", func(t *testing.T) {
		obj := newFakeSpreadObject()
		obj.nextReconcileTime = metav1.NewTime(time.Now().Add(-1 * time.Hour))
		assert.Equal(t, time.Duration(0), mgr.RequeueDelay(obj))
	})

	t.Run("future time returns remaining", func(t *testing.T) {
		obj := newFakeSpreadObject()
		obj.nextReconcileTime = metav1.NewTime(time.Now().Add(1 * time.Hour))
		delay := mgr.RequeueDelay(obj)
		assert.InDelta(t, float64(1*time.Hour), float64(delay), float64(2*time.Second))
	})
}

func TestSetNextReconcileTime(t *testing.T) {
	mgr := NewManager(
		WithMinDuration(1*time.Hour),
		WithMaxDuration(2*time.Hour),
	)

	obj := newFakeSpreadObject()
	mgr.SetNextReconcileTime(obj)

	nrt := obj.GetNextReconcileTime()
	assert.False(t, nrt.IsZero())

	minExpected := time.Now().Add(1 * time.Hour)
	maxExpected := time.Now().Add(2 * time.Hour)
	assert.True(t, nrt.After(minExpected.Add(-5*time.Second)), "next reconcile time should be after min")
	assert.True(t, nrt.Time.Before(maxExpected.Add(5*time.Second)), "next reconcile time should be before max")
}

func TestSetNextReconcileTime_EqualMinMax(t *testing.T) {
	mgr := NewManager(
		WithMinDuration(1*time.Hour),
		WithMaxDuration(1*time.Hour),
	)

	obj := newFakeSpreadObject()
	mgr.SetNextReconcileTime(obj)

	nrt := obj.GetNextReconcileTime()
	assert.False(t, nrt.IsZero())

	expected := time.Now().Add(1 * time.Hour)
	assert.InDelta(t, float64(expected.UnixMilli()), float64(nrt.UnixMilli()), float64(5*time.Second/time.Millisecond))
}

func TestUpdateObservedGeneration(t *testing.T) {
	mgr := NewManager()
	obj := newFakeSpreadObject()
	obj.Generation = 5
	obj.observedGeneration = 1

	mgr.UpdateObservedGeneration(obj)
	assert.Equal(t, int64(5), obj.GetObservedGeneration())
}

func TestRemoveRefreshLabel(t *testing.T) {
	mgr := NewManager()

	t.Run("removes label", func(t *testing.T) {
		obj := newFakeSpreadObject()
		obj.Labels = map[string]string{RefreshLabel: "true", "other": "value"}
		assert.True(t, mgr.RemoveRefreshLabel(obj))
		_, exists := obj.Labels[RefreshLabel]
		assert.False(t, exists)
		assert.Equal(t, "value", obj.Labels["other"])
	})

	t.Run("no label present", func(t *testing.T) {
		obj := newFakeSpreadObject()
		assert.False(t, mgr.RemoveRefreshLabel(obj))
	})

	t.Run("nil labels", func(t *testing.T) {
		obj := newFakeSpreadObject()
		obj.Labels = nil
		assert.False(t, mgr.RemoveRefreshLabel(obj))
	})
}
