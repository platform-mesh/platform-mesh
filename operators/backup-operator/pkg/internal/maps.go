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

// Package internal contains utilities shared across backup-operator packages.
package internal

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// SortedKeys returns the keys of m in ascending order.
func SortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	return slices.Sorted(maps.Keys(m))
}

// CombineErrors joins multiple errors into a single error, capping the total
// message at 4096 bytes so condition fields in Kubernetes status objects are
// not bloated when many shards fail simultaneously.
func CombineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	const maxMessageBytes = 4096
	joined := errors.Join(errs...).Error()
	if len(joined) <= maxMessageBytes {
		return errors.Join(errs...)
	}
	suffix := fmt.Sprintf(" (+%d more shard errors truncated)", len(errs)-1)
	budget := maxMessageBytes - len(suffix)
	if budget < 1 {
		budget = 1
	}
	first := errs[0].Error()
	if len(first) > budget {
		first = first[:budget]
	}
	return fmt.Errorf("%s%s", first, suffix)
}
