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
	"time"

	"go.platform-mesh.io/qbrtool/internal/models"
)

// QuarterFilter filters items by quarter
type QuarterFilter struct {
	quarter models.Quarter
}

// NewQuarterFilter creates a new quarter filter
func NewQuarterFilter(quarter models.Quarter) *QuarterFilter {
	return &QuarterFilter{quarter: quarter}
}

// Matches checks if the item falls within the quarter
// An item is considered to be in a quarter if:
// - It was created during or before the quarter end, AND
// - It was not closed before the quarter started
func (f *QuarterFilter) Matches(item *models.ProjectItem) bool {
	start := f.quarter.StartDate()
	end := f.quarter.EndDate()

	// Item must be created before or during the quarter
	if item.CreatedAt.After(end) {
		return false
	}

	// If item was closed, it must be closed after the quarter started
	if item.ClosedAt != nil && item.ClosedAt.Before(start) {
		return false
	}

	return true
}

// Name returns the name of the filter
func (f *QuarterFilter) Name() string {
	return "quarter"
}

// DateRangeFilter filters items by a custom date range
type DateRangeFilter struct {
	start time.Time
	end   time.Time
}

// NewDateRangeFilter creates a new date range filter
func NewDateRangeFilter(start, end time.Time) *DateRangeFilter {
	return &DateRangeFilter{start: start, end: end}
}

// Matches checks if the item falls within the date range
func (f *DateRangeFilter) Matches(item *models.ProjectItem) bool {
	// Item must be created before or during the range end
	if item.CreatedAt.After(f.end) {
		return false
	}

	// If item was closed, it must be closed after the range start
	if item.ClosedAt != nil && item.ClosedAt.Before(f.start) {
		return false
	}

	return true
}

// Name returns the name of the filter
func (f *DateRangeFilter) Name() string {
	return "date_range"
}

// CreatedInQuarterFilter filters items that were created during the quarter
type CreatedInQuarterFilter struct {
	quarter models.Quarter
}

// NewCreatedInQuarterFilter creates a filter for items created in the quarter
func NewCreatedInQuarterFilter(quarter models.Quarter) *CreatedInQuarterFilter {
	return &CreatedInQuarterFilter{quarter: quarter}
}

// Matches checks if the item was created during the quarter
func (f *CreatedInQuarterFilter) Matches(item *models.ProjectItem) bool {
	return f.quarter.Contains(item.CreatedAt)
}

// Name returns the name of the filter
func (f *CreatedInQuarterFilter) Name() string {
	return "created_in_quarter"
}

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
