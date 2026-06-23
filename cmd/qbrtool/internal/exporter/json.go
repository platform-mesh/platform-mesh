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

package exporter

import (
	"time"

	"go.platform-mesh.io/qbrtool/internal/models"
)

// Metadata contains export metadata
type Metadata struct {
	Organization    string    `json:"organization"`
	ProjectNumber   int       `json:"project_number"`
	Quarter         string    `json:"quarter,omitempty"`
	ItemTypes       []string  `json:"item_types,omitempty"`
	IncludeArchived bool      `json:"include_archived"`
	TotalItems      int       `json:"total_items"`
	ExportedAt      time.Time `json:"exported_at"`
}

// ExportResult is the result of an export operation
type ExportResult struct {
	Metadata Metadata              `json:"metadata"`
	Items    []*models.ProjectItem `json:"items"`
}

// NewExportResult creates a new export result with metadata
func NewExportResult(org string, projectNumber int, quarter string, itemTypes []string, includeArchived bool, items []*models.ProjectItem) *ExportResult {
	return &ExportResult{
		Metadata: Metadata{
			Organization:    org,
			ProjectNumber:   projectNumber,
			Quarter:         quarter,
			ItemTypes:       itemTypes,
			IncludeArchived: includeArchived,
			TotalItems:      len(items),
			ExportedAt:      time.Now(),
		},
		Items: items,
	}
}

// Summary returns a summary of the export result
type Summary struct {
	TotalItems    int            `json:"total_items"`
	ByType        map[string]int `json:"by_type"`
	ByState       map[string]int `json:"by_state"`
	ArchivedCount int            `json:"archived_count"`
	EpicCount     int            `json:"epic_count"`
}

// GetSummary returns a summary of the exported items
func (r *ExportResult) GetSummary() Summary {
	summary := Summary{
		TotalItems: len(r.Items),
		ByType:     make(map[string]int),
		ByState:    make(map[string]int),
	}

	for _, item := range r.Items {
		// Count by type
		summary.ByType[string(item.Type)]++

		// Count by state
		if item.State != "" {
			summary.ByState[item.State]++
		}

		// Count archived
		if item.IsArchived {
			summary.ArchivedCount++
		}

		// Count epics
		if item.IsEpic {
			summary.EpicCount++
		}
	}

	return summary
}
