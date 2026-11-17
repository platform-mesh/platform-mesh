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
	"go.platform-mesh.io/qbrtool/internal/models"
)

// ClosedInQuarterFilter filters items that were closed during the quarter
type ClosedInQuarterFilter struct {
	quarter models.Quarter
}

// NewClosedInQuarterFilter creates a filter for items closed in the quarter
func NewClosedInQuarterFilter(quarter models.Quarter) *ClosedInQuarterFilter {
	return &ClosedInQuarterFilter{quarter: quarter}
}

// Matches checks if the item was closed during the quarter
func (f *ClosedInQuarterFilter) Matches(item *models.ProjectItem) bool {
	if item.ClosedAt == nil {
		return false
	}
	return f.quarter.Contains(*item.ClosedAt)
}

// Name returns the name of the filter
func (f *ClosedInQuarterFilter) Name() string {
	return "closed_in_quarter"
}
