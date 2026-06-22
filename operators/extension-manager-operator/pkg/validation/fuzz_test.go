package validation

import (
	"encoding/json"
	"testing"

	"go.platform-mesh.io/extension-manager-operator/pkg/validation/validation_test"
)

// FuzzValidate fuzzes the content-configuration parse/validate boundary.
//
// Validate is the single function in this repo that turns untrusted bytes into
// a parsed configuration: it is reached from the HTTP /validate endpoint and
// from the controller reconcile path, which ingests both inline and remotely
// downloaded (fully attacker-influenced) configuration. The body asserts
// invariants rather than exact outputs:
//   - it must never panic on any input/content type;
//   - a successful result must be non-empty, valid JSON (the contract the
//     subroutine relies on when it later json.Unmarshals the result);
//   - an error result must be the empty string.
func FuzzValidate(f *testing.F) {
	// Seed corpus: known-valid fixtures plus the malformed inputs and content
	// types already exercised by the unit tests.
	f.Add(validation_test.GetValidJSON(), "json")
	f.Add(validation_test.GetValidYAML(), "yaml")
	f.Add(`{"name": "overview",`, "json")
	f.Add("!2", "yaml")
	f.Add("2!", "yaml")
	f.Add(validation_test.GetValidJSON(), "xml")
	f.Add("", "json")

	cC := NewContentConfiguration()

	f.Fuzz(func(t *testing.T, input string, contentType string) {
		result, merr := cC.Validate([]byte(input), contentType)

		if merr != nil && merr.Len() > 0 {
			if result != "" {
				t.Errorf("got error result, expected empty string, got %q", result)
			}
			return
		}

		if result == "" {
			t.Errorf("got success with empty result for input %q (contentType %q)", input, contentType)
			return
		}

		if !json.Valid([]byte(result)) {
			t.Errorf("successful result is not valid JSON: %q", result)
		}
	})
}
