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

package filter

import (
	"strings"

	"go.platform-mesh.io/qbrtool/internal/models"
)

// TypeFilter filters items by type
type TypeFilter struct {
	types []models.ItemType
}

// NewTypeFilter creates a new type filter
func NewTypeFilter(types []models.ItemType) *TypeFilter {
	return &TypeFilter{types: types}
}

// Matches checks if the item type matches any of the allowed types
func (f *TypeFilter) Matches(item *models.ProjectItem) bool {
	for _, t := range f.types {
		if item.Type == t {
			return true
		}
	}
	return false
}

// Name returns the name of the filter
func (f *TypeFilter) Name() string {
	return "type"
}

// EpicFilter filters only epic items
type EpicFilter struct{}

// NewEpicFilter creates a new epic filter
func NewEpicFilter() *EpicFilter {
	return &EpicFilter{}
}

// Matches checks if the item is an epic
func (f *EpicFilter) Matches(item *models.ProjectItem) bool {
	// Check IsEpic flag
	if item.IsEpic {
		return true
	}

	// Check field values for Type = Epic
	if typeVal, ok := item.FieldValues["Type"]; ok {
		if strings.EqualFold(typeVal, "epic") {
			return true
		}
	}

	// Check labels for epic
	for _, label := range item.Labels {
		lower := strings.ToLower(label)
		if lower == "epic" || lower == "type/epic" || lower == "kind/epic" {
			return true
		}
	}

	return false
}

// Name returns the name of the filter
func (f *EpicFilter) Name() string {
	return "epic"
}
