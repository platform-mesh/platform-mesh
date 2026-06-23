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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.platform-mesh.io/qbrtool/internal/analyzer"
	"go.platform-mesh.io/qbrtool/internal/exporter"
	"go.platform-mesh.io/qbrtool/internal/models"
)

var (
	inputFile     string
	analysisType  string
	ossOrgs       []string
	analyzeOutput string
	analyzeFormat string
	groupByFields []string
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Analyze exported project items",
	Long: `Analyze exported project board items for various categories.

Analysis types:
  - cve: Find items mentioning CVEs (CVE-YYYY-NNNNN pattern)
  - oss: Find OSS contributions to specific organizations
  - monitoring: Find monitoring/observability related items
  - lifecycle: Find lifecycle management related items
  - security: Find security related items
  - all: Run all analyzers

Dynamic grouping:
  Use --group-by to group items by any field value (from project board or built-in).
  Multiple --group-by flags create separate groupings for each field.

Output formats:
  - json: Detailed JSON output (default)
  - markdown/md: Markdown report grouped by analyzer

Examples:
  # Analyze for CVEs
  qbrtool analyze -i items.json --analysis cve

  # Run all analyzers with JSON output
  qbrtool analyze -i items.json --analysis all

  # Group items by Status field
  qbrtool analyze -i items.json --group-by Status

  # Group by multiple fields
  qbrtool analyze -i items.json --group-by Status --group-by IssueType

  # Combine with other analyzers
  qbrtool analyze -i items.json --analysis cve --group-by Status

  # Generate markdown report
  qbrtool analyze -i items.json --analysis all --format md -f report.md

  # Analyze from stdin
  cat items.json | qbrtool analyze --analysis all`,
	RunE: runAnalyze,
}

func init() {
	analyzeCmd.Flags().StringVarP(&inputFile, "input", "i", "", "Input JSON file (default: stdin)")
	analyzeCmd.Flags().StringVarP(&analysisType, "analysis", "a", "", "Analysis type: cve, oss, monitoring, lifecycle, security, all")
	analyzeCmd.Flags().StringArrayVar(&groupByFields, "group-by", nil, "Group items by field (e.g., Status, Type, IssueType)")
	analyzeCmd.Flags().StringSliceVar(&ossOrgs, "oss-orgs", []string{"kcp-dev", "kube-bind", "multicluster-runtime"}, "OSS organizations to detect")
	analyzeCmd.Flags().StringVarP(&analyzeOutput, "output-file", "f", "", "Output file path (default: stdout)")
	analyzeCmd.Flags().StringVarP(&analyzeFormat, "format", "F", "json", "Output format: json, markdown, md")
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	// Read input
	var input io.Reader = os.Stdin
	if inputFile != "" {
		f, err := os.Open(inputFile)
		if err != nil {
			return fmt.Errorf("failed to open input file: %w", err)
		}
		defer f.Close()
		input = f
	}

	data, err := io.ReadAll(input)
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	// Parse input JSON
	var exportResult exporter.ExportResult
	if err := json.Unmarshal(data, &exportResult); err != nil {
		return fmt.Errorf("failed to parse input JSON: %w", err)
	}

	items := exportResult.Items
	Log("Loaded %d items for analysis", len(items))

	// Create analyzers based on type and group-by fields
	analyzers := createAnalyzers(analysisType, ossOrgs, groupByFields)
	if len(analyzers) == 0 {
		return fmt.Errorf("no analyzers specified: use --analysis or --group-by")
	}

	// Run analyses
	results := make(map[string]*models.AnalysisResult)
	for _, a := range analyzers {
		Log("Running %s analyzer...", a.Name())
		result := a.Analyze(items)
		results[a.Name()] = result
		Log("%s: found %d matches", a.Name(), len(result.Items))
	}

	// Validate format
	format := strings.ToLower(analyzeFormat)
	if format != "json" && format != "markdown" && format != "md" {
		return fmt.Errorf("unknown output format: %s (supported: json, markdown, md)", analyzeFormat)
	}

	// Write output based on format
	if analyzeOutput != "" {
		f, err := os.Create(analyzeOutput)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer f.Close()

		if err := writeAnalyzeOutput(f, exportResult.Metadata, results, format); err != nil {
			return err
		}
		// Print summary to stderr
		printSummary(results)
	} else {
		if err := writeAnalyzeOutput(os.Stdout, exportResult.Metadata, results, format); err != nil {
			return err
		}
	}

	return nil
}

func writeAnalyzeOutput(w io.Writer, metadata exporter.Metadata, results map[string]*models.AnalysisResult, format string) error {
	switch format {
	case "markdown", "md":
		return analyzer.WriteMarkdown(w, metadata, results)
	default:
		output := AnalyzeOutput{
			SourceMetadata: metadata,
			Results:        results,
		}
		jsonOutput, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		_, err = w.Write(jsonOutput)
		if err != nil {
			return fmt.Errorf("failed to write JSON: %w", err)
		}
		// Add newline
		fmt.Fprintln(w)
		return nil
	}
}

type AnalyzeOutput struct {
	SourceMetadata exporter.Metadata                 `json:"source_metadata"`
	Results        map[string]*models.AnalysisResult `json:"results"`
}

func createAnalyzers(analysisType string, ossOrgs []string, groupByFields []string) []analyzer.Analyzer {
	var result []analyzer.Analyzer

	// Add standard analyzers based on type
	if analysisType != "" {
		types := strings.Split(strings.ToLower(analysisType), ",")
		for _, t := range types {
			t = strings.TrimSpace(t)
			switch t {
			case "all":
				result = append(result,
					analyzer.NewCVEAnalyzer(),
					analyzer.NewOSSAnalyzer(ossOrgs),
					analyzer.NewMonitoringAnalyzer(),
					analyzer.NewLifecycleAnalyzer(),
					analyzer.NewSecurityAnalyzer(),
				)
			case "cve":
				result = append(result, analyzer.NewCVEAnalyzer())
			case "oss":
				result = append(result, analyzer.NewOSSAnalyzer(ossOrgs))
			case "monitoring":
				result = append(result, analyzer.NewMonitoringAnalyzer())
			case "lifecycle":
				result = append(result, analyzer.NewLifecycleAnalyzer())
			case "security":
				result = append(result, analyzer.NewSecurityAnalyzer())
			}
		}
	}

	// Add group-by analyzers
	for _, field := range groupByFields {
		result = append(result, analyzer.NewGroupByAnalyzer(field))
	}

	return result
}

func printSummary(results map[string]*models.AnalysisResult) {
	fmt.Fprintln(os.Stderr, "\n=== Analysis Summary ===")
	for name, result := range results {
		fmt.Fprintf(os.Stderr, "%s: %d items found\n", name, len(result.Items))
	}
}
