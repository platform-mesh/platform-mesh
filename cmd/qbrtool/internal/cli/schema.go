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

	"github.com/spf13/cobra"
	"go.platform-mesh.io/qbrtool/internal/github"
	"go.platform-mesh.io/qbrtool/internal/models"
)

var (
	schemaOrg           string
	schemaProjectNumber int
	schemaOutputFile    string
)

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Dump project schema as JSON",
	Long: `Fetch and dump the complete schema of a GitHub ProjectV2.

This includes all custom fields, their types, and allowed values
(e.g., single-select options, iteration configurations).

The schema is useful for:
- Understanding what fields are available for filtering
- Discovering custom field types (Epic, Initiative, etc.)
- Building dynamic queries and analyses

Examples:
  # Dump schema to file
  qbrtool schema --org platform-mesh --project 1 -f schema.json

  # Dump to stdout
  qbrtool schema --org platform-mesh --project 1

  # Use default org/project
  qbrtool schema -f schema.json`,
	RunE: runSchema,
}

func init() {
	schemaCmd.Flags().StringVarP(&schemaOrg, "org", "o", "platform-mesh", "GitHub organization name")
	schemaCmd.Flags().IntVarP(&schemaProjectNumber, "project", "p", 1, "Project number")
	schemaCmd.Flags().StringVarP(&schemaOutputFile, "output-file", "f", "", "Output file path (default: stdout)")

	rootCmd.AddCommand(schemaCmd)
}

func runSchema(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Validate token
	ghToken := GetToken()
	if ghToken == "" {
		return fmt.Errorf("GitHub token required: set GITHUB_TOKEN env var or use --token flag")
	}

	// Create GitHub client
	client := github.NewClient(ghToken)

	Log("Fetching schema for %s/projects/%d", schemaOrg, schemaProjectNumber)

	// Fetch project schema
	schema, err := client.GetProjectSchema(ctx, schemaOrg, schemaProjectNumber)
	if err != nil {
		return fmt.Errorf("failed to fetch project schema: %w", err)
	}

	Log("Found %d fields", len(schema.Fields))

	// Marshal to JSON
	output, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}

	// Write output
	if schemaOutputFile != "" {
		if err := os.WriteFile(schemaOutputFile, output, 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Schema written to %s\n", schemaOutputFile)

		// Print summary to stderr
		printSchemaSummary(schema)
	} else {
		fmt.Println(string(output))
	}

	return nil
}

func printSchemaSummary(schema *models.ProjectSchema) {
	fmt.Fprintln(os.Stderr, "\n=== Schema Summary ===")
	fmt.Fprintf(os.Stderr, "Project: %s (#%d)\n", schema.Project.Title, schema.Project.Number)
	fmt.Fprintf(os.Stderr, "Organization: %s\n", schema.Project.Organization)
	fmt.Fprintf(os.Stderr, "Fields: %d\n\n", len(schema.Fields))

	for _, field := range schema.Fields {
		switch field.DataType {
		case models.FieldTypeSingleSelect:
			options := field.GetSelectOptions()
			fmt.Fprintf(os.Stderr, "  %s (single_select): %v\n", field.Name, options)
		case models.FieldTypeIteration:
			titles := field.GetIterationTitles()
			fmt.Fprintf(os.Stderr, "  %s (iteration): %d iterations\n", field.Name, len(titles))
		default:
			fmt.Fprintf(os.Stderr, "  %s (%s)\n", field.Name, field.DataType)
		}
	}
}
