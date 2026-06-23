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

// MonitoringAnalyzer detects monitoring/observability related items
type MonitoringAnalyzer struct {
	keywords []string
	pattern  *regexp.Regexp
}

// NewMonitoringAnalyzer creates a new monitoring analyzer
func NewMonitoringAnalyzer() *MonitoringAnalyzer {
	keywords := []string{
		"monitoring",
		"observability",
		"metrics",
		"prometheus",
		"grafana",
		"alerting",
		"alerts",
		"logging",
		"logs",
		"tracing",
		"traces",
		"opentelemetry",
		"otel",
		"jaeger",
		"zipkin",
		"dashboard",
		"dashboards",
		"SLO",
		"SLI",
		"SLA",
		"telemetry",
		"instrumentation",
		"APM",
		"application performance",
		"health check",
		"healthcheck",
		"liveness",
		"readiness",
		"probe",
		"datadog",
		"newrelic",
		"splunk",
		"elasticsearch",
		"kibana",
		"fluentd",
		"fluentbit",
		"loki",
		"tempo",
		"mimir",
		"thanos",
		"cortex",
	}

	// Build pattern
	escapedKeywords := make([]string, len(keywords))
	for i, kw := range keywords {
		escapedKeywords[i] = regexp.QuoteMeta(kw)
	}
	pattern := regexp.MustCompile(`(?i)\b(` + strings.Join(escapedKeywords, "|") + `)\b`)

	return &MonitoringAnalyzer{
		keywords: keywords,
		pattern:  pattern,
	}
}

// Name returns the analyzer name
func (a *MonitoringAnalyzer) Name() string {
	return "monitoring"
}

// Analyze searches for monitoring-related items
func (a *MonitoringAnalyzer) Analyze(items []*models.ProjectItem) *models.AnalysisResult {
	var matches []models.MatchedItem
	keywordCounts := make(map[string]int)

	for _, item := range items {
		matchInfo := a.findMonitoringContent(item)
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
		Type:  "monitoring",
		Items: matches,
		Summary: models.KeywordAnalysis{
			MatchedKeywords: keywordCounts,
			Total:           len(matches),
		},
		Timestamp: time.Now(),
	}
}

// findMonitoringContent searches for monitoring keywords
func (a *MonitoringAnalyzer) findMonitoringContent(item *models.ProjectItem) *models.MatchInfo {
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

// uniqueStrings returns unique strings from a slice
func uniqueStrings(strs []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(strs))
	for _, s := range strs {
		lower := strings.ToLower(s)
		if !seen[lower] {
			seen[lower] = true
			result = append(result, s)
		}
	}
	return result
}
