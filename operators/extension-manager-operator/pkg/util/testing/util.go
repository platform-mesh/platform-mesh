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

package testing

import (
	"encoding/json"
	"fmt"

	"github.com/google/go-cmp/cmp"
)

// CompareJSON This function is used to compare two JSON strings in unit tests
func CompareJSON(json1, json2 string) (bool, error) { // coverage-ignore
	var obj1, obj2 map[string]interface{}

	err := json.Unmarshal([]byte(json1), &obj1)
	if err != nil {
		return false, err
	}

	err = json.Unmarshal([]byte(json2), &obj2)
	if err != nil {
		return false, err
	}

	equal := cmp.Equal(obj1, obj2)
	if !equal {
		diff := cmp.Diff(obj1, obj2)
		if diff != "" {
			fmt.Printf("Differences:\n%s", diff)
		}
	}
	return equal, nil
}
