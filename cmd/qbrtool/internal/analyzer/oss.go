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
	"regexp"
	"strings"
	"time"

	"go.platform-mesh.io/qbrtool/internal/models"
)

// OSSAnalyzer detects contributions to OSS organizations
type OSSAnalyzer struct {
	targetOrgs []string
	patterns   map[string]*regexp.Regexp
}

// NewOSSAnalyzer creates a new OSS analyzer for the given organizations
func NewOSSAnalyzer(orgs []string) *OSSAnalyzer {
	a := &OSSAnalyzer{
		targetOrgs: orgs,
		patterns:   make(map[string]*regexp.Regexp),
	}

	// Build patterns for each org
	for _, org := range orgs {
		// Match:
		// - github.com/org/
		// - org/ (as repo prefix)
		// - "upstream org" mentions
		// - "contributed to org"
		// - PR/issue URLs containing the org
		pattern := regexp.MustCompile(fmt.Sprintf(
			`(?i)(github\.com/%s/[^\s\)\"']+|(?:^|\s)%s/[a-zA-Z0-9_-]+|upstream[:\s]+%s|contribut\w*\s+(?:to\s+)?%s)`,
			regexp.QuoteMeta(org),
			regexp.QuoteMeta(org),
			regexp.QuoteMeta(org),
			regexp.QuoteMeta(org),
		))
		a.patterns[org] = pattern
	}

	return a
}

// Name returns the analyzer name
func (a *OSSAnalyzer) Name() string {
	return "oss"
}

// Analyze searches for OSS contribution references
func (a *OSSAnalyzer) Analyze(items []*models.ProjectItem) *models.AnalysisResult {
	var matches []models.MatchedItem
	byOrg := make(map[string]int)

	for _, item := range items {
		matchInfo := a.findOSSContribution(item)
		if matchInfo != nil {
			// Extract the org from match info
			org := a.extractOrg(matchInfo.MatchedText)
			if org != "" {
				byOrg[org]++
			}

			matches = append(matches, models.MatchedItem{
				Item:      *item,
				MatchInfo: *matchInfo,
			})
		}
	}

	total := 0
	for _, count := range byOrg {
		total += count
	}

	return &models.AnalysisResult{
		Type:  "oss",
		Items: matches,
		Summary: models.OSSAnalysis{
			ByOrg: byOrg,
			Total: total,
		},
		Timestamp: time.Now(),
	}
}

// findOSSContribution searches for OSS contribution patterns
func (a *OSSAnalyzer) findOSSContribution(item *models.ProjectItem) *models.MatchInfo {
	// First check if repository owner is one of our target orgs
	if item.Repository.Owner != "" {
		for _, org := range a.targetOrgs {
			if strings.EqualFold(item.Repository.Owner, org) {
				return &models.MatchInfo{
					MatchedIn:   "repository",
					MatchedText: fmt.Sprintf("org: %s, repo: %s", org, item.Repository.FullName()),
					Confidence:  1.0,
				}
			}
		}
	}

	// Check URL for OSS org references
	for org, pattern := range a.patterns {
		if matches := pattern.FindAllString(item.URL, -1); len(matches) > 0 {
			return &models.MatchInfo{
				MatchedIn:   "url",
				MatchedText: fmt.Sprintf("org: %s, match: %s", org, strings.Join(matches, ", ")),
				Confidence:  1.0,
			}
		}
	}

	// Check title and body
	searchText := item.Title + " " + item.Body

	for org, pattern := range a.patterns {
		if matches := pattern.FindAllString(searchText, -1); len(matches) > 0 {
			return &models.MatchInfo{
				MatchedIn:   "content",
				MatchedText: fmt.Sprintf("org: %s, match: %s", org, strings.Join(matches, ", ")),
				Confidence:  0.8,
			}
		}
	}

	// Check labels
	for _, label := range item.Labels {
		for _, org := range a.targetOrgs {
			if strings.Contains(strings.ToLower(label), strings.ToLower(org)) {
				return &models.MatchInfo{
					MatchedIn:   "labels",
					MatchedText: fmt.Sprintf("org: %s, label: %s", org, label),
					Confidence:  0.9,
				}
			}
		}
	}

	return nil
}

// extractOrg extracts the organization name from match text
func (a *OSSAnalyzer) extractOrg(matchText string) string {
	for _, org := range a.targetOrgs {
		if strings.Contains(strings.ToLower(matchText), strings.ToLower(org)) {
			return org
		}
	}
	return ""
}
