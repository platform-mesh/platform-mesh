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

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.platform-mesh.io/qbrtool/internal/exporter"
	"go.platform-mesh.io/qbrtool/internal/filter"
	"go.platform-mesh.io/qbrtool/internal/github"
	"go.platform-mesh.io/qbrtool/internal/models"
)

var (
	org             string
	projectNumber   int
	quarter         string
	itemTypes       []string
	includeArchived bool
	outputFile      string
	exportFormat    string
	fullExport      bool
	fieldFilters    []string
	orgFilter       string
	filterOrgs      []string
	listFields      bool
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export project board items to JSON or CSV",
	Long: `Export items from a GitHub Project Board to JSON or CSV format.

Supports filtering by quarter (e.g., Q3-2025), item types (issue, pr, draft, epic),
and custom project field values using --field.
Can include archived items using the --include-archived flag.
Use --full to include all custom field values from the project board.

Field filter syntax:
  --field "Field=Value"       Exact match (case-insensitive)
  --field "Field!=Value"      Not equal
  --field "Field~Value"       Contains
  --field "Field=A,B,C"       Match any of the values

Organization filter:
  --org-filter internal       Only items from project's org
  --org-filter external       Only items from external orgs
  --org-filter all            All items (default)
  --filter-orgs org1,org2     Only items from specific orgs

Examples:
  # Export all items from Q3-2025 as JSON
  qbrtool export --quarter Q3-2025 -f q3-2025.json

  # Export with full field values (Status, Type, Priority, etc.)
  qbrtool export --quarter Q3-2025 --full -f q3-2025.json

  # Filter by project field values (requires --full for custom fields)
  qbrtool export --full --field "Type=Epic" -f epics.json
  qbrtool export --full --field "Status=Done" --field "Type=Initiative" -f done-initiatives.json

  # Export only external contributions (from other orgs)
  qbrtool export --full --org-filter external -f external-contributions.json

  # Export only items from specific orgs
  qbrtool export --full --filter-orgs kcp-dev,kube-bind -f kcp-items.json

  # Export as CSV for easy reading
  qbrtool export --quarter Q3-2025 --format csv -f q3-2025.csv

  # Export only issues including archived
  qbrtool export --type issue --include-archived -f issues.json

  # Export to stdout
  qbrtool export --quarter Q4-2025

  # List available fields for a project
  qbrtool export --org platform-mesh --project 1 --list-fields`,
	RunE: runExport,
}

func init() {
	exportCmd.Flags().StringVarP(&org, "org", "o", "platform-mesh", "GitHub organization name")
	exportCmd.Flags().IntVarP(&projectNumber, "project", "p", 1, "Project number")
	exportCmd.Flags().StringVarP(&quarter, "quarter", "q", "", "Quarter filter (e.g., Q3-2025, Q1-2025)")
	exportCmd.Flags().StringSliceVarP(&itemTypes, "type", "t", nil, "Item types to include (issue, pr, draft, epic)")
	exportCmd.Flags().StringArrayVar(&fieldFilters, "field", nil, "Filter by field value (e.g., Type=Epic, Status!=Done)")
	exportCmd.Flags().StringVar(&orgFilter, "org-filter", "all", "Filter by org: all, internal, external")
	exportCmd.Flags().StringSliceVar(&filterOrgs, "filter-orgs", nil, "Filter to specific orgs (comma-separated)")
	exportCmd.Flags().BoolVar(&includeArchived, "include-archived", false, "Include archived items")
	exportCmd.Flags().BoolVar(&fullExport, "full", false, "Include all custom field values from project board")
	exportCmd.Flags().BoolVar(&listFields, "list-fields", false, "List available fields from project schema and exit")
	exportCmd.Flags().StringVarP(&outputFile, "output-file", "f", "", "Output file path (default: stdout)")
	exportCmd.Flags().StringVarP(&exportFormat, "format", "F", "json", "Output format: json, csv")
}

func runExport(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Validate token
	ghToken := GetToken()
	if ghToken == "" {
		return fmt.Errorf("GitHub token required: set GITHUB_TOKEN env var or use --token flag")
	}

	// Create GitHub client
	client := github.NewClient(ghToken)

	// Handle --list-fields: fetch schema and display available fields
	if listFields {
		Log("Fetching project schema for %s/projects/%d", org, projectNumber)
		schema, err := client.GetProjectSchema(ctx, org, projectNumber)
		if err != nil {
			return fmt.Errorf("failed to get project schema: %w", err)
		}
		printProjectFields(schema)
		return nil
	}

	Log("Fetching project ID for %s/projects/%d", org, projectNumber)

	// Get project info
	projectID, err := client.GetProjectID(ctx, org, projectNumber)
	if err != nil {
		return fmt.Errorf("failed to get project ID: %w", err)
	}
	Log("Project ID: %s", projectID)

	// Fetch current (non-archived) items
	if fullExport {
		Log("Fetching current project items with full field values...")
	} else {
		Log("Fetching current project items...")
	}
	items, err := client.GetProjectItemsFull(ctx, projectID, fullExport)
	if err != nil {
		return fmt.Errorf("failed to get project items: %w", err)
	}
	Log("Fetched %d current items", len(items))

	// Fetch archived items if requested
	if includeArchived {
		Log("Fetching archived items via search...")
		var q *models.Quarter
		if quarter != "" {
			parsed, err := models.ParseQuarter(quarter)
			if err != nil {
				return fmt.Errorf("invalid quarter format: %w", err)
			}
			q = &parsed
		}
		archivedItems, err := client.SearchArchivedItems(ctx, org, projectNumber, q, items, verbose)
		if err != nil {
			return fmt.Errorf("failed to search archived items: %w", err)
		}
		Log("Found %d archived items", len(archivedItems))
		items = mergeItems(items, archivedItems)
		Log("Total items after merge: %d", len(items))
	}

	// Apply filters
	var filters []filter.Filter

	// Quarter filter - by default, show only items closed in the quarter
	if quarter != "" {
		q, err := models.ParseQuarter(quarter)
		if err != nil {
			return fmt.Errorf("invalid quarter format: %w", err)
		}
		filters = append(filters, filter.NewClosedInQuarterFilter(q))
		Log("Applying quarter filter (closed in %s)", quarter)
	}

	// Type filter
	if len(itemTypes) > 0 {
		types := make([]models.ItemType, 0, len(itemTypes))
		for _, t := range itemTypes {
			switch strings.ToLower(t) {
			case "issue":
				types = append(types, models.ItemTypeIssue)
			case "pr", "pullrequest", "pull_request":
				types = append(types, models.ItemTypePullRequest)
			case "draft", "draftissue", "draft_issue":
				types = append(types, models.ItemTypeDraftIssue)
			case "epic":
				// Epic is handled separately via IsEpic field
				filters = append(filters, filter.NewEpicFilter())
			default:
				return fmt.Errorf("unknown item type: %s", t)
			}
		}
		if len(types) > 0 {
			filters = append(filters, filter.NewTypeFilter(types))
		}
		Log("Applying type filter: %v", itemTypes)
	}

	// Dynamic field filters
	if len(fieldFilters) > 0 {
		dynamicFilters, err := filter.ParseFieldFilters(fieldFilters)
		if err != nil {
			return fmt.Errorf("invalid field filter: %w", err)
		}
		for _, df := range dynamicFilters {
			filters = append(filters, df)
			Log("Applying field filter: %s", df.String())
		}
	}

	// Organization filter
	if len(filterOrgs) > 0 {
		// Filter to specific orgs
		filters = append(filters, filter.NewSpecificOrgFilter(filterOrgs))
		Log("Applying org filter: specific orgs %v", filterOrgs)
	} else if orgFilter != "all" {
		switch strings.ToLower(orgFilter) {
		case "internal":
			filters = append(filters, filter.NewInternalOrgFilter(org))
			Log("Applying org filter: internal (org=%s)", org)
		case "external":
			filters = append(filters, filter.NewExternalOrgFilter(org))
			Log("Applying org filter: external (not org=%s)", org)
		default:
			return fmt.Errorf("unknown org-filter value: %s (valid: all, internal, external)", orgFilter)
		}
	}

	// Apply all filters
	if len(filters) > 0 {
		chain := filter.NewChain(filters, filter.ModeAND)
		items = chain.Apply(items)
		Log("Items after filtering: %d", len(items))
	}

	// Prepare export result
	result := exporter.ExportResult{
		Metadata: exporter.Metadata{
			Organization:    org,
			ProjectNumber:   projectNumber,
			Quarter:         quarter,
			ItemTypes:       itemTypes,
			IncludeArchived: includeArchived,
			TotalItems:      len(items),
		},
		Items: items,
	}

	// Validate format
	format := strings.ToLower(exportFormat)
	if format != "json" && format != "csv" {
		return fmt.Errorf("unknown output format: %s (supported: json, csv)", exportFormat)
	}

	// Write output based on format
	if outputFile != "" {
		f, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer f.Close()

		if err := writeExportOutput(f, &result, format); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Exported %d items to %s\n", len(items), outputFile)
	} else {
		if err := writeExportOutput(os.Stdout, &result, format); err != nil {
			return err
		}
	}

	return nil
}

func writeExportOutput(w *os.File, result *exporter.ExportResult, format string) error {
	switch format {
	case "csv":
		return exporter.WriteCSV(w, result)
	default:
		output, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		_, err = w.Write(output)
		if err != nil {
			return fmt.Errorf("failed to write JSON: %w", err)
		}
		// Add newline for stdout
		if w == os.Stdout {
			fmt.Println()
		}
		return nil
	}
}

// mergeItems merges current and archived items, deduplicating by ID
func mergeItems(current, archived []*models.ProjectItem) []*models.ProjectItem {
	seen := make(map[string]bool)
	result := make([]*models.ProjectItem, 0, len(current)+len(archived))

	// Current items take precedence
	for _, item := range current {
		if !seen[item.ID] {
			seen[item.ID] = true
			result = append(result, item)
		}
	}

	// Add archived items not already present
	for _, item := range archived {
		if !seen[item.ID] {
			seen[item.ID] = true
			result = append(result, item)
		}
	}

	return result
}

// printProjectFields displays available fields from the project schema
func printProjectFields(schema *models.ProjectSchema) {
	fmt.Printf("Available fields for %s/%d (%s):\n\n", schema.Project.Organization, schema.Project.Number, schema.Project.Title)

	for _, field := range schema.Fields {
		dataType := strings.ToLower(string(field.DataType))

		switch field.DataType {
		case models.FieldTypeSingleSelect:
			options := field.GetSelectOptions()
			if len(options) > 0 {
				fmt.Printf("  %s (%s): %s\n", field.Name, dataType, strings.Join(options, ", "))
			} else {
				fmt.Printf("  %s (%s)\n", field.Name, dataType)
			}

		case models.FieldTypeIteration:
			titles := field.GetIterationTitles()
			if len(titles) > 5 {
				// Show first 5 iterations and indicate more
				fmt.Printf("  %s (%s): %s, ... (%d more)\n", field.Name, dataType, strings.Join(titles[:5], ", "), len(titles)-5)
			} else if len(titles) > 0 {
				fmt.Printf("  %s (%s): %s\n", field.Name, dataType, strings.Join(titles, ", "))
			} else {
				fmt.Printf("  %s (%s)\n", field.Name, dataType)
			}

		default:
			fmt.Printf("  %s (%s)\n", field.Name, dataType)
		}
	}

	fmt.Println()
	fmt.Println("Built-in fields (always available for filtering):")
	fmt.Println("  state: open, closed, merged")
	fmt.Println("  type: issue, pr, draft")
	fmt.Println("  repo: repository name")
	fmt.Println("  author: username")
	fmt.Println("  milestone: milestone title")
	fmt.Println()
	fmt.Println("Usage examples:")
	fmt.Printf("  qbrtool export --org %s --project %d --full --field \"Status=Done\"\n", schema.Project.Organization, schema.Project.Number)
	fmt.Printf("  qbrtool export --org %s --project %d --full --field \"Type=Epic\"\n", schema.Project.Organization, schema.Project.Number)
}
