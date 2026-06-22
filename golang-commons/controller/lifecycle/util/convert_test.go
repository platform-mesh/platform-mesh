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

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/api"
	pmtesting "go.platform-mesh.io/golang-commons/controller/testSupport"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
)

func TestToRuntimeObjectSpreadReconcileStatusInterface_Success(t *testing.T) {
	tl := testlogger.New()
	apiObject := &pmtesting.ImplementingSpreadReconciles{}
	obj, err := ToInterface[api.RuntimeObjectSpreadReconcileStatus](apiObject, tl.Logger)
	assert.NoError(t, err)
	assert.NotNil(t, obj)
}

func TestToRuntimeObjectSpreadReconcileStatusInterface_Failure(t *testing.T) {
	tl := testlogger.New()
	// DummyRuntimeObject does NOT implement RuntimeObjectSpreadReconcileStatus
	apiObject := &pmtesting.DummyRuntimeObject{}
	_, err := ToInterface[api.RuntimeObjectSpreadReconcileStatus](apiObject, tl.Logger)
	assert.Error(t, err)

	messages, logErr := tl.GetLogMessages()
	assert.NoError(t, logErr)
	assert.Contains(t, messages[0].Message, "Failed to cast instance to target interface")
}

func TestMustToRuntimeObjectSpreadReconcileStatusInterface_Success(t *testing.T) {
	tl := testlogger.New()
	apiObject := &pmtesting.ImplementingSpreadReconciles{}
	obj := MustToInterface[api.RuntimeObjectSpreadReconcileStatus](apiObject, tl.Logger)
	assert.NotNil(t, obj)
}

func TestMustToRuntimeObjectSpreadReconcileStatusInterface_Panic(t *testing.T) {
	tl := testlogger.New()
	apiObject := &pmtesting.DummyRuntimeObject{}
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic but did not panic")
		}
	}()
	MustToInterface[api.RuntimeObjectSpreadReconcileStatus](apiObject, tl.Logger)
}
