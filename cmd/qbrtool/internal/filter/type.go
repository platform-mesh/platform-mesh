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

// StateFilter filters items by state
type StateFilter struct {
	states []string
}

// NewStateFilter creates a new state filter
func NewStateFilter(states []string) *StateFilter {
	return &StateFilter{states: states}
}

// Matches checks if the item state matches any of the allowed states
func (f *StateFilter) Matches(item *models.ProjectItem) bool {
	for _, s := range f.states {
		if strings.EqualFold(item.State, s) {
			return true
		}
	}
	return false
}

// Name returns the name of the filter
func (f *StateFilter) Name() string {
	return "state"
}

// ArchivedFilter filters archived or non-archived items
type ArchivedFilter struct {
	archived bool
}

// NewArchivedFilter creates a filter for archived items
func NewArchivedFilter(archived bool) *ArchivedFilter {
	return &ArchivedFilter{archived: archived}
}

// Matches checks if the item matches the archived state
func (f *ArchivedFilter) Matches(item *models.ProjectItem) bool {
	return item.IsArchived == f.archived
}

// Name returns the name of the filter
func (f *ArchivedFilter) Name() string {
	return "archived"
}

// RepositoryFilter filters items by repository
type RepositoryFilter struct {
	repos []string // format: "owner/name"
}

// NewRepositoryFilter creates a filter for specific repositories
func NewRepositoryFilter(repos []string) *RepositoryFilter {
	return &RepositoryFilter{repos: repos}
}

// Matches checks if the item belongs to one of the allowed repositories
func (f *RepositoryFilter) Matches(item *models.ProjectItem) bool {
	fullName := item.Repository.FullName()
	if fullName == "" {
		return false
	}

	for _, repo := range f.repos {
		if strings.EqualFold(fullName, repo) {
			return true
		}
	}
	return false
}

// Name returns the name of the filter
func (f *RepositoryFilter) Name() string {
	return "repository"
}

// LabelFilter filters items by label
type LabelFilter struct {
	labels []string
	mode   ChainMode // AND or OR for multiple labels
}

// NewLabelFilter creates a filter for items with specific labels
func NewLabelFilter(labels []string, mode ChainMode) *LabelFilter {
	return &LabelFilter{labels: labels, mode: mode}
}

// Matches checks if the item has the required labels
func (f *LabelFilter) Matches(item *models.ProjectItem) bool {
	if f.mode == ModeAND {
		for _, required := range f.labels {
			found := false
			for _, label := range item.Labels {
				if strings.EqualFold(label, required) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}

	// ModeOR
	for _, required := range f.labels {
		for _, label := range item.Labels {
			if strings.EqualFold(label, required) {
				return true
			}
		}
	}
	return false
}

// Name returns the name of the filter
func (f *LabelFilter) Name() string {
	return "label"
}
