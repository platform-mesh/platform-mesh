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

package util

import (
	"fmt"
	"os"
	"strings"
)

func RemoveEmptyStrings(slice []string) []string {
	var result []string
	for _, s := range slice {
		if len(strings.TrimSpace(s)) > 0 {
			result = append(result, strings.TrimSpace(s))
		}
	}
	return result
}
func ContainString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func PrintErrOut(msg string, err error) {
	_, printErr := fmt.Fprintln(os.Stderr, msg, err)
	if printErr != nil { // coverage-ignore
		// Fallback is to print to stdout instead
		fmt.Println(msg, err)
	}
}
