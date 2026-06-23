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

package directive

import (
	"encoding/json"
	"testing"
)

func FuzzExtractResourceContext(f *testing.F) {
	f.Add(`{"context":{"group":"core.platform-mesh.io","kind":"Account","resource":{"name":"test"},"accountPath":"root:org"}}`)
	f.Add(`{"context":{"group":"","kind":"","resource":{"name":"","namespace":"ns"},"accountPath":""}}`)
	f.Add(`{"context":{}}`)
	f.Add(`{}`)
	f.Add(`{"context":"not-an-object"}`)
	f.Add(`{"wrongKey":true}`)
	f.Add(`not json`)
	f.Add(``)
	f.Add(`null`)
	f.Add(`{"context":null}`)

	f.Fuzz(func(t *testing.T, input string) {
		var args map[string]any
		if err := json.Unmarshal([]byte(input), &args); err != nil {
			return
		}
		// Must not panic regardless of map contents.
		_, _ = extractResourceContextFromArguments(args)
	})
}
