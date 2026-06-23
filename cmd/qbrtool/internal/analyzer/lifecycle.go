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
	"regexp"
	"strings"
	"time"

	"go.platform-mesh.io/qbrtool/internal/models"
)

// LifecycleAnalyzer detects lifecycle management related items
type LifecycleAnalyzer struct {
	keywords []string
	pattern  *regexp.Regexp
}

// NewLifecycleAnalyzer creates a new lifecycle analyzer
func NewLifecycleAnalyzer() *LifecycleAnalyzer {
	keywords := []string{
		"lifecycle",
		"life cycle",
		"upgrade",
		"upgrades",
		"upgrading",
		"migration",
		"migrate",
		"migrating",
		"deprecation",
		"deprecated",
		"deprecate",
		"EOL",
		"end of life",
		"end-of-life",
		"maintenance",
		"maintain",
		"release",
		"releases",
		"releasing",
		"version",
		"versioning",
		"versions",
		"rollout",
		"roll out",
		"rolling update",
		"rollback",
		"roll back",
		"canary",
		"blue-green",
		"blue green",
		"deployment strategy",
		"progressive delivery",
		"feature flag",
		"feature flags",
		"feature toggle",
		"sunset",
		"sunsetting",
		"decommission",
		"decommissioning",
		"retirement",
		"retiring",
		"LTS",
		"long term support",
		"patch",
		"patches",
		"patching",
		"hotfix",
		"backport",
		"backporting",
		"semantic versioning",
		"semver",
		"breaking change",
		"breaking changes",
		"backward compatible",
		"backwards compatible",
	}

	// Build pattern
	escapedKeywords := make([]string, len(keywords))
	for i, kw := range keywords {
		escapedKeywords[i] = regexp.QuoteMeta(kw)
	}
	pattern := regexp.MustCompile(`(?i)\b(` + strings.Join(escapedKeywords, "|") + `)\b`)

	return &LifecycleAnalyzer{
		keywords: keywords,
		pattern:  pattern,
	}
}

// Name returns the analyzer name
func (a *LifecycleAnalyzer) Name() string {
	return "lifecycle"
}

// Analyze searches for lifecycle management related items
func (a *LifecycleAnalyzer) Analyze(items []*models.ProjectItem) *models.AnalysisResult {
	var matches []models.MatchedItem
	keywordCounts := make(map[string]int)

	for _, item := range items {
		matchInfo := a.findLifecycleContent(item)
		if matchInfo != nil {
			// Count matched keywords
			for _, match := range strings.Split(matchInfo.MatchedText, ", ") {
				keywordCounts[strings.ToLower(match)]++
			}

			matches = append(matches, models.MatchedItem{
				Item:      *item,
				MatchInfo: *matchInfo,
			})
		}
	}

	return &models.AnalysisResult{
		Type:  "lifecycle",
		Items: matches,
		Summary: models.KeywordAnalysis{
			MatchedKeywords: keywordCounts,
			Total:           len(matches),
		},
		Timestamp: time.Now(),
	}
}

// findLifecycleContent searches for lifecycle keywords
func (a *LifecycleAnalyzer) findLifecycleContent(item *models.ProjectItem) *models.MatchInfo {
	// Check title
	if matches := a.pattern.FindAllString(item.Title, -1); len(matches) > 0 {
		return &models.MatchInfo{
			MatchedIn:   "title",
			MatchedText: strings.Join(uniqueStrings(matches), ", "),
			Confidence:  1.0,
		}
	}

	// Check labels
	for _, label := range item.Labels {
		if matches := a.pattern.FindAllString(label, -1); len(matches) > 0 {
			return &models.MatchInfo{
				MatchedIn:   "labels",
				MatchedText: strings.Join(uniqueStrings(matches), ", "),
				Confidence:  1.0,
			}
		}
	}

	// Check body (lower confidence)
	if matches := a.pattern.FindAllString(item.Body, -1); len(matches) > 0 {
		return &models.MatchInfo{
			MatchedIn:   "body",
			MatchedText: strings.Join(uniqueStrings(matches), ", "),
			Confidence:  0.7,
		}
	}

	return nil
}
