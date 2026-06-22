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
	"github.com/stretchr/testify/mock"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pmtesting "go.platform-mesh.io/golang-commons/controller/testSupport"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
)

func TestGetNextReconcilationTime(t *testing.T) {
	expectedEarliest := 12 * time.Hour
	expectedLatest := 24 * time.Hour

	actual := getNextReconcileTime(defaultMaxReconcileDuration)
	if actual < expectedEarliest || actual > expectedLatest {
		t.Errorf("Expected time between %v and %v, but got %v", expectedEarliest, expectedLatest, actual)
	}

	actual2 := getNextReconcileTime(defaultMaxReconcileDuration)
	if actual2 < expectedEarliest || actual2 > expectedLatest {
		t.Errorf("Expected time between %v and %v, but got %v", expectedEarliest, expectedLatest, actual)
	}

	if actual == actual2 {
		t.Errorf("Expected different values, but got same")
	}
}

func TestOnNextReconcile(t *testing.T) {
	nextReconcile := time.Now().Add(10 * time.Minute)
	instanceStatusObj := pmtesting.TestStatus{
		NextReconcileTime: v1.NewTime(nextReconcile),
	}
	s := NewSpreader()
	apiObject := &pmtesting.ImplementingSpreadReconciles{TestApiObject: pmtesting.TestApiObject{Status: instanceStatusObj}}
	tl := testlogger.New()

	requeueAfter, err := s.OnNextReconcile(apiObject, tl.Logger)
	if err != nil {
		t.Errorf("Expected no error, but got %v", err)
	}

	timeTill := time.Until(nextReconcile.UTC())
	if time.Duration(requeueAfter.RequeueAfter.Seconds()) != time.Duration(timeTill.Seconds()) {
		t.Errorf("Expected requeueAfter to be %v, but got %v", timeTill, requeueAfter.RequeueAfter)
	}
	messages, err := tl.GetLogMessages()
	assert.NoError(t, err)
	assert.Contains(t, messages[0].Message, "no processing needed")
}

type testInstance struct {
	mock.Mock
	*pmtesting.ImplementingSpreadReconciles
}

func (t *testInstance) GenerateNextReconcileTime() time.Duration {
	args := t.Called()
	return args.Get(0).(time.Duration)
}

func TestGenerateNextReconcileTimer(t *testing.T) {
	instance := &testInstance{
		ImplementingSpreadReconciles: &pmtesting.ImplementingSpreadReconciles{},
	}
	s := NewSpreader()
	instance.On("GenerateNextReconcileTime").Return(10 * time.Minute)

	s.SetNextReconcileTime(instance, testlogger.New().Logger)

	assert.True(t, instance.AssertCalled(t, "GenerateNextReconcileTime"))
}

func TestUpdateObservedGeneration(t *testing.T) {
	s := NewSpreader()
	instanceStatusObj := pmtesting.TestStatus{
		ObservedGeneration: 0,
	}
	apiObject := &pmtesting.ImplementingSpreadReconciles{TestApiObject: pmtesting.TestApiObject{
		Status: instanceStatusObj,
		ObjectMeta: v1.ObjectMeta{
			Generation: 1,
		},
	},
	}
	tl := testlogger.New()
	s.UpdateObservedGeneration(apiObject, tl.Logger)

	assert.Equal(t, apiObject.GetObservedGeneration(), apiObject.GetGeneration())
	messages, err := tl.GetLogMessages()
	assert.NoError(t, err)
	assert.Contains(t, messages[0].Message, "Updating observed generation")
}

func TestRemoveRefreshLabel(t *testing.T) {
	s := NewSpreader()
	apiObject := &pmtesting.TestApiObject{
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{ReconcileRefreshLabel: ""},
		},
	}
	s.RemoveRefreshLabelIfExists(apiObject)

	_, ok := apiObject.GetLabels()[ReconcileRefreshLabel]
	assert.False(t, ok)
}

func TestRemoveRefreshLabelFilledWithValue(t *testing.T) {
	s := NewSpreader()
	apiObject := &pmtesting.TestApiObject{
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{ReconcileRefreshLabel: "true"},
		},
	}
	s.RemoveRefreshLabelIfExists(apiObject)

	_, ok := apiObject.GetLabels()[ReconcileRefreshLabel]

	assert.False(t, ok)
}

func TestRemoveRefreshLabelNoLabels(t *testing.T) {
	s := NewSpreader()
	apiObject := &pmtesting.TestApiObject{
		ObjectMeta: v1.ObjectMeta{},
	}
	s.RemoveRefreshLabelIfExists(apiObject)

	_, ok := apiObject.GetLabels()[ReconcileRefreshLabel]

	assert.False(t, ok)
}

func TestReconcileRequired(t *testing.T) {
	s := NewSpreader()
	tl := testlogger.New()

	now := time.Now()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	// Case 1: Generation changed
	apiObject1 := &pmtesting.ImplementingSpreadReconciles{
		TestApiObject: pmtesting.TestApiObject{
			ObjectMeta: v1.ObjectMeta{
				Generation: 2,
			},
			Status: pmtesting.TestStatus{
				ObservedGeneration: 1,
				NextReconcileTime:  v1.NewTime(future),
			},
		},
	}
	assert.True(t, s.ReconcileRequired(apiObject1, tl.Logger), "Should require reconcile when generation changed")

	// Case 2: After next reconcile time
	apiObject2 := &pmtesting.ImplementingSpreadReconciles{
		TestApiObject: pmtesting.TestApiObject{
			ObjectMeta: v1.ObjectMeta{
				Generation: 1,
			},
			Status: pmtesting.TestStatus{
				ObservedGeneration: 1,
				NextReconcileTime:  v1.NewTime(past),
			},
		},
	}
	assert.True(t, s.ReconcileRequired(apiObject2, tl.Logger), "Should require reconcile when after next reconcile time")

	// Case 3: Refresh label present
	apiObject3 := &pmtesting.ImplementingSpreadReconciles{
		TestApiObject: pmtesting.TestApiObject{
			ObjectMeta: v1.ObjectMeta{
				Generation: 1,
				Labels:     map[string]string{ReconcileRefreshLabel: ""},
			},
			Status: pmtesting.TestStatus{
				ObservedGeneration: 1,
				NextReconcileTime:  v1.NewTime(future),
			},
		},
	}
	assert.True(t, s.ReconcileRequired(apiObject3, tl.Logger), "Should require reconcile when refresh label present")

	// Case 4: No condition met
	apiObject4 := &pmtesting.ImplementingSpreadReconciles{
		TestApiObject: pmtesting.TestApiObject{
			ObjectMeta: v1.ObjectMeta{
				Generation: 1,
			},
			Status: pmtesting.TestStatus{
				ObservedGeneration: 1,
				NextReconcileTime:  v1.NewTime(future),
			},
		},
	}
	assert.False(t, s.ReconcileRequired(apiObject4, tl.Logger), "Should not require reconcile when no condition met")
}
