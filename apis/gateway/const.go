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

package apis

import "time"

const (
	CategoriesExtensionKey     = "x-kubernetes-categories"
	GVKExtensionKey            = "x-kubernetes-group-version-kind"
	ScopeExtensionKey          = "x-kubernetes-scope"
	PrinterColumnsExtensionKey = "x-kubernetes-print-columns"

	// Timeout constants for different test scenarios
	ShortTimeout = 100 * time.Millisecond // Short timeout for quick operations
	LongTimeout  = 2 * time.Second        // Longer timeout for file system operations
)
