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
	"io"
	"sort"
	"strings"
	"time"

	"go.platform-mesh.io/qbrtool/internal/exporter"
	"go.platform-mesh.io/qbrtool/internal/models"
)

// WriteMarkdown writes analysis results as a markdown report.
func WriteMarkdown(w io.Writer, metadata exporter.Metadata, results map[string]*models.AnalysisResult) error {
	// Write header
	fmt.Fprintf(w, "# Quarterly Analysis Report\n\n")

	// Write metadata section
	fmt.Fprintf(w, "## Source Information\n\n")
	fmt.Fprintf(w, "| Field | Value |\n")
	fmt.Fprintf(w, "|-------|-------|\n")
	fmt.Fprintf(w, "| Organization | %s |\n", metadata.Organization)
	fmt.Fprintf(w, "| Project | #%d |\n", metadata.ProjectNumber)
	if metadata.Quarter != "" {
		fmt.Fprintf(w, "| Quarter | %s |\n", metadata.Quarter)
	}
	if len(metadata.ItemTypes) > 0 {
		fmt.Fprintf(w, "| Item Types | %s |\n", strings.Join(metadata.ItemTypes, ", "))
	}
	fmt.Fprintf(w, "| Total Items | %d |\n", metadata.TotalItems)
	fmt.Fprintf(w, "| Include Archived | %t |\n", metadata.IncludeArchived)
	fmt.Fprintf(w, "| Generated | %s |\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "\n---\n\n")

	// Sort analyzer names for consistent output
	analyzerNames := make([]string, 0, len(results))
	for name := range results {
		analyzerNames = append(analyzerNames, name)
	}
	sort.Strings(analyzerNames)

	// Write each analyzer section
	for _, name := range analyzerNames {
		result := results[name]
		if err := writeAnalyzerSection(w, name, result); err != nil {
			return err
		}
	}

	return nil
}

func writeAnalyzerSection(w io.Writer, name string, result *models.AnalysisResult) error {
	// Section header
	title := formatAnalyzerTitle(name)
	fmt.Fprintf(w, "## %s\n\n", title)

	// Summary based on analyzer type
	writeSummary(w, name, result)

	// Item count
	if len(result.Items) == 0 {
		fmt.Fprintf(w, "No items found.\n\n")
		return nil
	}

	fmt.Fprintf(w, "Found **%d** items.\n\n", len(result.Items))

	// Items table
	fmt.Fprintf(w, "| # | Title | URL | Match |\n")
	fmt.Fprintf(w, "|---|-------|-----|-------|\n")

	for _, matched := range result.Items {
		item := matched.Item
		matchInfo := matched.MatchInfo

		// Format URL as markdown link
		urlDisplay := "-"
		if item.URL != "" {
			urlDisplay = fmt.Sprintf("[#%d](%s)", item.Number, item.URL)
		}

		// Escape pipe characters in title and match text
		title := escapeMdTableCell(item.Title)
		matchText := escapeMdTableCell(formatMatchInfo(matchInfo))

		// Truncate long titles
		if len(title) > 60 {
			title = title[:57] + "..."
		}

		fmt.Fprintf(w, "| %d | %s | %s | %s |\n",
			item.Number,
			title,
			urlDisplay,
			matchText,
		)
	}

	fmt.Fprintf(w, "\n")
	return nil
}

func writeSummary(w io.Writer, name string, result *models.AnalysisResult) {
	if result.Summary == nil {
		return
	}

	switch name {
	case "cve":
		if summary, ok := result.Summary.(map[string]interface{}); ok {
			if cveIDs, ok := summary["cve_ids"].([]interface{}); ok && len(cveIDs) > 0 {
				fmt.Fprintf(w, "**CVEs found:** ")
				ids := make([]string, len(cveIDs))
				for i, id := range cveIDs {
					ids[i] = fmt.Sprintf("`%v`", id)
				}
				fmt.Fprintf(w, "%s\n\n", strings.Join(ids, ", "))
			}
		}
	case "oss":
		if summary, ok := result.Summary.(map[string]interface{}); ok {
			if byOrg, ok := summary["by_org"].(map[string]interface{}); ok && len(byOrg) > 0 {
				fmt.Fprintf(w, "**Contributions by organization:**\n\n")
				// Sort org names
				orgs := make([]string, 0, len(byOrg))
				for org := range byOrg {
					orgs = append(orgs, org)
				}
				sort.Strings(orgs)
				for _, org := range orgs {
					count := byOrg[org]
					fmt.Fprintf(w, "- **%s**: %.0f items\n", org, count)
				}
				fmt.Fprintf(w, "\n")
			}
		}
	case "monitoring", "lifecycle", "security":
		if summary, ok := result.Summary.(map[string]interface{}); ok {
			if keywords, ok := summary["matched_keywords"].(map[string]interface{}); ok && len(keywords) > 0 {
				fmt.Fprintf(w, "**Top matched keywords:**\n\n")
				// Sort and get top 10 keywords
				type kv struct {
					k string
					v float64
				}
				var kvs []kv
				for k, v := range keywords {
					if vf, ok := v.(float64); ok {
						kvs = append(kvs, kv{k, vf})
					}
				}
				sort.Slice(kvs, func(i, j int) bool { return kvs[i].v > kvs[j].v })
				limit := 10
				if len(kvs) < limit {
					limit = len(kvs)
				}
				for i := 0; i < limit; i++ {
					fmt.Fprintf(w, "- `%s`: %.0f\n", kvs[i].k, kvs[i].v)
				}
				fmt.Fprintf(w, "\n")
			}
		}
	default:
		// Handle group-by analyzers
		if strings.HasPrefix(name, "group-by-") {
			writeGroupBySummary(w, result)
		}
	}
}

// writeGroupBySummary writes a summary table for group-by analysis
func writeGroupBySummary(w io.Writer, result *models.AnalysisResult) {
	summary, ok := result.Summary.(*GroupBySummary)
	if !ok {
		return
	}

	// Sort groups by count (descending)
	type groupEntry struct {
		name  string
		stats GroupStats
	}
	var groups []groupEntry
	for name, stats := range summary.Groups {
		groups = append(groups, groupEntry{name, stats})
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].stats.Count > groups[j].stats.Count
	})

	// Check if we have sub-groups (org grouping)
	hasSubGroups := false
	for _, g := range groups {
		if len(g.stats.SubGroups) > 0 {
			hasSubGroups = true
			break
		}
	}

	if hasSubGroups {
		// Org grouping with repos column
		fmt.Fprintf(w, "| %s | Count | Repos |\n", summary.FieldName)
		fmt.Fprintf(w, "|-----|-------|-------|\n")
		for _, g := range groups {
			reposStr := formatSubGroups(g.stats.SubGroups)
			fmt.Fprintf(w, "| %s | %d | %s |\n", g.name, g.stats.Count, reposStr)
		}
	} else {
		// Standard group-by table
		fmt.Fprintf(w, "| %s | Count |\n", summary.FieldName)
		fmt.Fprintf(w, "|-----|-------|\n")
		for _, g := range groups {
			fmt.Fprintf(w, "| %s | %d |\n", g.name, g.stats.Count)
		}
	}
	fmt.Fprintf(w, "\n")
}

// formatSubGroups formats repo sub-groups as "repo1 (n), repo2 (n), ..."
func formatSubGroups(subGroups map[string]int) string {
	if len(subGroups) == 0 {
		return "-"
	}

	// Sort by count descending
	type sg struct {
		name  string
		count int
	}
	var sorted []sg
	for name, count := range subGroups {
		sorted = append(sorted, sg{name, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	// Format as "repo (count), ..."
	var parts []string
	for _, s := range sorted {
		parts = append(parts, fmt.Sprintf("%s (%d)", s.name, s.count))
	}
	return strings.Join(parts, ", ")
}

func formatAnalyzerTitle(name string) string {
	switch name {
	case "cve":
		return "CVE Analysis"
	case "oss":
		return "OSS Contributions"
	case "monitoring":
		return "Monitoring & Observability"
	case "lifecycle":
		return "Lifecycle Management"
	case "security":
		return "Security Analysis"
	default:
		// Capitalize first letter manually to avoid deprecated strings.Title
		if len(name) > 0 {
			return strings.ToUpper(name[:1]) + name[1:] + " Analysis"
		}
		return "Analysis"
	}
}

func formatMatchInfo(info models.MatchInfo) string {
	if info.MatchedText == "" {
		return info.MatchedIn
	}
	text := info.MatchedText
	if len(text) > 40 {
		text = text[:37] + "..."
	}
	return fmt.Sprintf("%s: %s", info.MatchedIn, text)
}

func escapeMdTableCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
