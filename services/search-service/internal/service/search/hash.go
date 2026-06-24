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

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

func queryHash(q string) string {
	h := sha256.Sum256([]byte(q))
	return hex.EncodeToString(h[:])
}

func filtersHash(filters map[string][]string) string {
	if len(filters) == 0 {
		return ""
	}

	keys := make([]string, 0, len(filters))
	for key := range filters {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		rawValues := filters[key]
		values := make([]string, 0, len(rawValues))
		for _, value := range rawValues {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				values = append(values, trimmed)
			}
		}
		sort.Strings(values)
		if len(values) == 0 {
			continue
		}
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strings.Join(values, ","))
		b.WriteString(";")
	}

	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:])
}
