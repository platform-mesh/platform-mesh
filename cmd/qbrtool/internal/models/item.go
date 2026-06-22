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

package models

import "time"

// ItemType represents the type of a project item
type ItemType string

const (
	ItemTypeIssue       ItemType = "ISSUE"
	ItemTypePullRequest ItemType = "PULL_REQUEST"
	ItemTypeDraftIssue  ItemType = "DRAFT_ISSUE"
)

// ProjectItem represents a unified project board item
type ProjectItem struct {
	// Identifiers
	ID            string `json:"id"`
	ProjectItemID string `json:"project_item_id,omitempty"`

	// Type and status
	Type       ItemType `json:"type"`
	IsArchived bool     `json:"is_archived"`
	State      string   `json:"state,omitempty"` // OPEN, CLOSED, MERGED

	// Content fields
	Number int    `json:"number,omitempty"`
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	URL    string `json:"url,omitempty"`

	// Dates
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at,omitempty"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	MergedAt  *time.Time `json:"merged_at,omitempty"` // PR only

	// Relationships
	Repository Repository `json:"repository,omitempty"`
	Labels     []string   `json:"labels,omitempty"`
	Assignees  []string   `json:"assignees,omitempty"`
	Author     string     `json:"author,omitempty"`
	Milestone  *Milestone `json:"milestone,omitempty"`

	// PR-specific stats
	PRStats *PRStats `json:"pr_stats,omitempty"`

	// Project-specific fields (custom fields from the project board)
	FieldValues map[string]string `json:"field_values,omitempty"`

	// Analysis metadata
	IsEpic bool `json:"is_epic,omitempty"`
}

// Milestone represents a GitHub milestone
type Milestone struct {
	Title string     `json:"title"`
	DueOn *time.Time `json:"due_on,omitempty"`
}

// PRStats holds pull request statistics
type PRStats struct {
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
	ChangedFiles int `json:"changed_files"`
}

// Repository represents a GitHub repository
type Repository struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// FullName returns the full repository name (owner/name)
func (r Repository) FullName() string {
	if r.Owner == "" || r.Name == "" {
		return ""
	}
	return r.Owner + "/" + r.Name
}

// MatchInfo contains information about how an item matched a search
type MatchInfo struct {
	MatchedIn   string  `json:"matched_in"`   // "title", "body", "labels"
	MatchedText string  `json:"matched_text"` // The actual matched text
	Confidence  float64 `json:"confidence,omitempty"`
}

// MatchedItem represents an item that matched an analysis
type MatchedItem struct {
	Item      ProjectItem `json:"item"`
	MatchInfo MatchInfo   `json:"match_info"`
}

// AnalysisResult represents the result of an analysis
type AnalysisResult struct {
	Type      string        `json:"type"`
	Items     []MatchedItem `json:"items"`
	Summary   interface{}   `json:"summary,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

// CVEAnalysis is the summary for CVE analysis
type CVEAnalysis struct {
	CVEIDs []string `json:"cve_ids"`
	Count  int      `json:"count"`
}

// OSSAnalysis is the summary for OSS contribution analysis
type OSSAnalysis struct {
	ByOrg map[string]int `json:"by_org"`
	Total int            `json:"total"`
}

// KeywordAnalysis is the summary for keyword-based analysis
type KeywordAnalysis struct {
	MatchedKeywords map[string]int `json:"matched_keywords"`
	Total           int            `json:"total"`
}
