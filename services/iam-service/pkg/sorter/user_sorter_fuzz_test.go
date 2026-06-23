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

package sorter

import (
	"testing"
)

func FuzzParseUserSortField(f *testing.F) {
	f.Add("lastName")
	f.Add("last_name")
	f.Add("USERID")
	f.Add("email")
	f.Add("firstName")
	f.Add("")
	f.Add("unknown_field")

	f.Fuzz(func(t *testing.T, input string) {
		_ = parseUserSortField(input)
	})
}

func FuzzParseSortDirection(f *testing.F) {
	f.Add("ASC")
	f.Add("DESC")
	f.Add("asc")
	f.Add("ASCENDING")
	f.Add("DESCENDING")
	f.Add("")
	f.Add("invalid")

	f.Fuzz(func(t *testing.T, input string) {
		_ = parseSortDirection(input)
	})
}
