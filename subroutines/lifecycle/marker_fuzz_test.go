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

package lifecycle

import (
	"context"
	"testing"
)

// FuzzRemoveMarkerFromStatus fuzzes the status-marker parsing helpers, which
// process initializer/terminator markers set externally (by kcp) on an object's
// status. It checks two invariants on arbitrary marker strings:
//
//  1. removeMarkerFromStatus reports success exactly when the marker was present
//     (its return value agrees with hasMarkerInStatus).
//  2. After removal the marker is gone (hasMarkerInStatus returns false).
//
// Neither helper may panic on any input.
func FuzzRemoveMarkerFromStatus(f *testing.F) {
	f.Add(statusFieldInitializers, "setup", "setup")
	f.Add(statusFieldTerminators, "teardown", "other")
	f.Add(statusFieldInitializers, "", "")
	f.Add(statusFieldInitializers, "a", "b")
	f.Add(statusFieldTerminators, "marker-with-dashes", "marker-with-dashes")

	f.Fuzz(func(t *testing.T, field, seed, target string) {
		// Only two status fields are recognized; map anything else onto a valid
		// one so the unstructured round-trip exercises real code paths.
		if field != statusFieldInitializers && field != statusFieldTerminators {
			field = statusFieldInitializers
		}

		obj := &testObject{}
		switch field {
		case statusFieldInitializers:
			obj.Status.Initializers = []string{seed}
		case statusFieldTerminators:
			obj.Status.Terminators = []string{seed}
		}

		ctx := context.Background()

		had := hasMarkerInStatus(ctx, obj, field, target)
		removed := removeMarkerFromStatus(ctx, obj, field, target)

		if had != removed {
			t.Fatalf("hasMarkerInStatus=%v but removeMarkerFromStatus=%v (field=%q seed=%q target=%q)",
				had, removed, field, seed, target)
		}

		if hasMarkerInStatus(ctx, obj, field, target) {
			t.Fatalf("marker %q still present after removal (field=%q seed=%q)", target, field, seed)
		}
	})
}
