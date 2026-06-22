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

package conditions

import (
	"errors"
	"testing"

	"go.platform-mesh.io/subroutines"

	"k8s.io/apimachinery/pkg/api/meta"
)

// FuzzSetSubroutineCondition fuzzes the mapping from an arbitrary subroutine
// name and message into a status condition. SetSubroutineCondition must never
// panic on arbitrary input, and the resulting condition must be retrievable by
// its derived type (name, optionally suffixed with "Finalize") and carry the
// message back unchanged.
func FuzzSetSubroutineCondition(f *testing.F) {
	f.Add("sub1", "all good", false, false)
	f.Add("sub1", "boom", false, true)
	f.Add("", "", true, false)
	f.Add("name-with-dashes", "msg with spaces", true, true)

	f.Fuzz(func(t *testing.T, name, msg string, isFinalize, withErr bool) {
		mgr := NewManager()
		obj := &fakeConditionObject{}

		// Both branches produce a condition whose Message equals msg: the error
		// branch uses err.Error(), the skip branch uses the result message.
		var err error
		result := subroutines.Skip(msg)
		if withErr {
			err = errors.New(msg)
		}

		mgr.SetSubroutineCondition(obj, name, result, err, isFinalize)

		condName := name
		if isFinalize {
			condName = name + "Finalize"
		}

		cond := meta.FindStatusCondition(obj.GetConditions(), condName)
		if cond == nil {
			t.Fatalf("condition %q not found after SetSubroutineCondition (name=%q isFinalize=%v)", condName, name, isFinalize)
		}
		if cond.Message != msg {
			t.Fatalf("condition message = %q, want %q", cond.Message, msg)
		}
	})
}
