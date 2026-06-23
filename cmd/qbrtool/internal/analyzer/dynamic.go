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

package analyzer

import (
	"fmt"
	"strings"
	"time"

	"go.platform-mesh.io/qbrtool/internal/models"
)

// GroupByAnalyzer groups items by a specified field value
type GroupByAnalyzer struct {
	fieldName string
}

// NewGroupByAnalyzer creates a new group-by analyzer for the specified field
func NewGroupByAnalyzer(fieldName string) *GroupByAnalyzer {
	return &GroupByAnalyzer{
		fieldName: fieldName,
	}
}

// Name returns the name of the analyzer
func (a *GroupByAnalyzer) Name() string {
	return fmt.Sprintf("group-by-%s", strings.ToLower(a.fieldName))
}

// Analyze groups items by the specified field and returns results
func (a *GroupByAnalyzer) Analyze(items []*models.ProjectItem) *models.AnalysisResult {
	groups := make(map[string][]*models.ProjectItem)

	for _, item := range items {
		value := a.getFieldValue(item)
		if value == "" {
			value = "(empty)"
		}
		groups[value] = append(groups[value], item)
	}

	// Convert to MatchedItems for consistent output
	var matchedItems []models.MatchedItem
	for _, groupItems := range groups {
		for _, item := range groupItems {
			matchedItems = append(matchedItems, models.MatchedItem{
				Item: *item,
				MatchInfo: models.MatchInfo{
					MatchedIn:   a.fieldName,
					MatchedText: a.getFieldValue(item),
				},
			})
		}
	}

	// Create summary with group counts
	summary := &GroupBySummary{
		FieldName: a.fieldName,
		Groups:    make(map[string]GroupStats),
		Total:     len(items),
	}

	isOrgGrouping := strings.ToLower(a.fieldName) == "org"

	for value, groupItems := range groups {
		stats := GroupStats{
			Count: len(groupItems),
			Items: extractItemRefs(groupItems),
		}

		// Track repo sub-groups when grouping by org
		if isOrgGrouping {
			stats.SubGroups = make(map[string]int)
			for _, item := range groupItems {
				if item.Repository.Name != "" {
					stats.SubGroups[item.Repository.Name]++
				}
			}
		}

		summary.Groups[value] = stats
	}

	return &models.AnalysisResult{
		Type:      a.Name(),
		Items:     matchedItems,
		Summary:   summary,
		Timestamp: time.Now(),
	}
}

// getFieldValue gets the value of the specified field from the item
func (a *GroupByAnalyzer) getFieldValue(item *models.ProjectItem) string {
	// First check FieldValues map (custom project fields)
	if value, ok := item.FieldValues[a.fieldName]; ok {
		return value
	}

	// Check common built-in fields (case-insensitive)
	switch strings.ToLower(a.fieldName) {
	case "status":
		if value, ok := item.FieldValues["Status"]; ok {
			return value
		}
		return item.State
	case "state":
		return item.State
	case "type":
		if value, ok := item.FieldValues["Type"]; ok {
			return value
		}
		return string(item.Type)
	case "issuetype":
		if value, ok := item.FieldValues["IssueType"]; ok {
			return value
		}
	case "repository", "repo":
		if item.Repository.Owner != "" {
			return item.Repository.FullName()
		}
		return ""
	case "org":
		return item.Repository.Owner
	case "author":
		return item.Author
	case "milestone":
		if item.Milestone != nil {
			return item.Milestone.Title
		}
	case "labels":
		if len(item.Labels) > 0 {
			return strings.Join(item.Labels, ", ")
		}
	case "assignees":
		if len(item.Assignees) > 0 {
			return strings.Join(item.Assignees, ", ")
		}
	}

	return ""
}

// GroupBySummary contains the summary of a group-by analysis
type GroupBySummary struct {
	FieldName string                `json:"field_name"`
	Groups    map[string]GroupStats `json:"groups"`
	Total     int                   `json:"total"`
}

// GroupStats contains statistics for a group
type GroupStats struct {
	Count     int            `json:"count"`
	Items     []ItemRef      `json:"items,omitempty"`
	SubGroups map[string]int `json:"sub_groups,omitempty"` // repo -> count (for org grouping)
}

// ItemRef is a minimal reference to an item
type ItemRef struct {
	ID     string `json:"id"`
	Number int    `json:"number,omitempty"`
	Title  string `json:"title"`
	URL    string `json:"url,omitempty"`
}

// extractItemRefs extracts minimal item references
func extractItemRefs(items []*models.ProjectItem) []ItemRef {
	refs := make([]ItemRef, len(items))
	for i, item := range items {
		refs[i] = ItemRef{
			ID:     item.ID,
			Number: item.Number,
			Title:  item.Title,
			URL:    item.URL,
		}
	}
	return refs
}

// MultiGroupByAnalyzer groups items by multiple fields
type MultiGroupByAnalyzer struct {
	fieldNames []string
}

// NewMultiGroupByAnalyzer creates a new multi-field group-by analyzer
func NewMultiGroupByAnalyzer(fieldNames []string) *MultiGroupByAnalyzer {
	return &MultiGroupByAnalyzer{
		fieldNames: fieldNames,
	}
}

// Name returns the name of the analyzer
func (a *MultiGroupByAnalyzer) Name() string {
	return fmt.Sprintf("group-by-%s", strings.Join(a.fieldNames, "-"))
}

// Analyze groups items by multiple fields
func (a *MultiGroupByAnalyzer) Analyze(items []*models.ProjectItem) *models.AnalysisResult {
	// Run individual group-by analyzers for each field
	summaries := make(map[string]*GroupBySummary)

	for _, fieldName := range a.fieldNames {
		analyzer := NewGroupByAnalyzer(fieldName)
		result := analyzer.Analyze(items)
		if summary, ok := result.Summary.(*GroupBySummary); ok {
			summaries[fieldName] = summary
		}
	}

	return &models.AnalysisResult{
		Type:      a.Name(),
		Items:     nil, // No individual items for multi-group
		Summary:   summaries,
		Timestamp: time.Now(),
	}
}
