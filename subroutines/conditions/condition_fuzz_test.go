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
