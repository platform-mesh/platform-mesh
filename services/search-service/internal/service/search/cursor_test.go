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

package search

import "testing"

func TestCursorRoundTrip(t *testing.T) {
	state := CursorState{
		Version:     cursorVersion,
		Org:         "acme",
		QueryHash:   queryHash("foo"),
		Mode:        SearchModeSemantic,
		Limit:       20,
		SearchAfter: []any{1.23, "abc"},
	}

	encoded, err := EncodeCursor(state)
	if err != nil {
		t.Fatalf("EncodeCursor returned error: %v", err)
	}

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor returned error: %v", err)
	}

	if decoded.Org != state.Org || decoded.QueryHash != state.QueryHash || decoded.Mode != state.Mode || decoded.Limit != state.Limit {
		t.Fatalf("decoded cursor mismatch: %+v", decoded)
	}
	if len(decoded.SearchAfter) != 2 {
		t.Fatalf("unexpected search_after length: %d", len(decoded.SearchAfter))
	}
}

func TestValidateCursorMismatch(t *testing.T) {
	state := CursorState{Version: cursorVersion, Org: "acme", QueryHash: queryHash("foo"), Mode: SearchModeLexical, Limit: 20, SearchAfter: []any{1.0, "x"}}
	if err := ValidateCursor(state, "other", queryHash("foo"), SearchModeLexical, "", "", 20); err == nil {
		t.Fatalf("expected org mismatch error")
	}
	if err := ValidateCursor(state, "acme", queryHash("bar"), SearchModeLexical, "", "", 20); err == nil {
		t.Fatalf("expected query mismatch error")
	}
	if err := ValidateCursor(state, "acme", queryHash("foo"), SearchModeSemantic, "", "", 20); err == nil {
		t.Fatalf("expected mode mismatch error")
	}
	if err := ValidateCursor(state, "acme", queryHash("foo"), SearchModeLexical, "", "", 30); err == nil {
		t.Fatalf("expected limit mismatch error")
	}
}
