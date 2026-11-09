# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
make build          # Build binary to ./bin/qbrtool
make build-debug    # Build with debug symbols
make test           # Run tests with race detection
make test-coverage  # Run tests with coverage report (outputs coverage.html)
make lint           # Run golangci-lint
make vet            # Run go vet
make install        # Install to $GOPATH/bin
make deps           # Download dependencies
make tidy           # Tidy go.mod
make clean          # Clean build artifacts
make run ARGS="..." # Run binary with arguments
make help           # Show all targets
```

**Note**: No test files (`*_test.go`) currently exist in the codebase.

## Running the Tool

Requires `GITHUB_TOKEN` environment variable (or `--token` flag):
```bash
export GITHUB_TOKEN=$(gh auth token)  # If using GitHub CLI

# Discover project schema
./bin/qbrtool schema -f schema.json

# List available fields
./bin/qbrtool export --list-fields

# Export project items with all custom fields
./bin/qbrtool export --full --quarter Q4-2025 -f output.json

# Export with dynamic field filtering
./bin/qbrtool export --full --field "Status=Done" --field "Priority=P0"

# Analyze with grouping and markdown output
./bin/qbrtool analyze -i output.json --analysis all --group-by Status --format md

# Pipeline: export and analyze
./bin/qbrtool export --full --quarter Q4-2025 | ./bin/qbrtool analyze --analysis all --format md
```

## CLI Commands

### schema
Dump complete project schema (fields, types, allowed values).
```bash
qbrtool schema --org <org> --project <num> -f schema.json
```

### export
Export project board items with filtering.
```bash
qbrtool export [flags]
```
Key flags:
- `--org`, `-o`: GitHub organization (default: "platform-mesh")
- `--project`, `-p`: Project number (default: 1)
- `--quarter`, `-q`: Filter by quarter (e.g., Q4-2025)
- `--type`, `-t`: Item types: issue, pr, draft, epic
- `--full`: Include all custom field values
- `--field`: Dynamic field filter (repeatable, see syntax below)
- `--org-filter`: Filter by org: all, internal, external
- `--filter-orgs`: Specific orgs (comma-separated)
- `--include-archived`: Include archived items
- `--list-fields`: List available fields and exit
- `--output-file`, `-f`: Output file (default: stdout)
- `--format`, `-F`: Output format: json, csv

**Field Filter Syntax**:
- `Field=Value` - Exact match (case-insensitive)
- `Field!=Value` - Not equal
- `Field~Value` - Contains
- `Field=A,B,C` - Match any value

### analyze
Analyze exported items.
```bash
qbrtool analyze [flags]
```
Key flags:
- `--input`, `-i`: Input JSON file (default: stdin)
- `--analysis`, `-a`: Analyzer: all, cve, oss, security, monitoring, lifecycle
- `--group-by`: Group by field (repeatable)
- `--oss-orgs`: OSS orgs to detect
- `--output-file`, `-f`: Output file (default: stdout)
- `--format`, `-F`: Output format: json, markdown, md

## Architecture

Go CLI tool built with Cobra that exports GitHub Project Board items and analyzes them for quarterly reports.

### Package Structure

- `cmd/qbrtool/` - Entry point, calls `cli.Execute()`
- `internal/cli/` - Cobra commands:
  - `root.go` - Global flags, token handling
  - `schema.go` - Schema discovery command
  - `export.go` - Export command with field filtering
  - `analyze.go` - Analyze command with grouping
- `internal/github/` - GraphQL client wrapping `shurcooL/graphql`:
  - `client.go` - Base client with OAuth2
  - `project.go` - Project queries (GetProjectID, GetProjectItems, GetProjectItemsFull)
  - `search.go` - Search API for archived items workaround
  - `schema.go` - Schema fetching (GetProjectSchema, field definitions)
- `internal/models/` - Domain types:
  - `item.go` - `ProjectItem`, `MatchedItem`, `AnalysisResult`
  - `quarter.go` - Quarter parsing and date range logic
  - `schema.go` - `ProjectSchema`, `FieldSchema`, `FieldDataType`, `SelectOption`
- `internal/filter/` - Filter interface and implementations:
  - `filter.go` - `Filter` interface, `Chain` with AND/OR modes
  - `time.go` - `ClosedInQuarterFilter`
  - `type.go` - `TypeFilter`, `EpicFilter`
  - `dynamic.go` - `DynamicFieldFilter` (operators: =, !=, ~), `OrgFilter` (internal/external/specific)
- `internal/analyzer/` - Analyzer interface and implementations:
  - `analyzer.go` - `Analyzer` interface
  - `cve.go` - CVE detection analyzer
  - `oss.go` - OSS contribution analyzer
  - `security.go` - Security-related item analyzer
  - `monitoring.go` - Monitoring/observability analyzer
  - `lifecycle.go` - Item lifecycle analyzer
  - `dynamic.go` - `GroupByAnalyzer` for dynamic grouping
  - `markdown.go` - `WriteMarkdown()` for report generation
- `internal/exporter/` - Export formatting:
  - `json.go` - JSON export with `Metadata`, `ExportResult`, `Summary`
  - `csv.go` - CSV export (`WriteCSV()`)

### Key Patterns

**Filter Chain**: Filters implement `Filter` interface with `Matches(item)` and `Name()` methods. Combined via `filter.Chain` with AND/OR modes.

**Dynamic Field Filtering**: `DynamicFieldFilter` in `filter/dynamic.go` supports filtering by any ProjectV2 custom field with operators (equals, not-equals, contains, in). Falls back to built-in fields (state, type, repo, author, milestone).

**Analyzers**: Analyzers implement `Analyzer` interface with `Name()` and `Analyze(items)` methods.

**Group-By Analysis**: `GroupByAnalyzer` in `analyzer/dynamic.go` groups items by any field value. Supports multi-field grouping and org→repo sub-grouping.

**Schema Discovery**: `GetProjectSchema()` fetches all ProjectV2 field definitions including types, options for single-select fields, and iteration configurations.

**Archived Items Workaround**: GitHub's ProjectV2 API doesn't return archived items. The tool uses search API queries split by month (to avoid 1000-result limit) and checks `projectItems` connection on found issues/PRs.

### Data Flow

1. `schema` command fetches project field definitions via GraphQL
2. `export` command fetches project items via GraphQL (with `--full` for custom fields)
3. Optionally searches for archived items via Search API
4. Applies filter chain (quarter, type, dynamic fields, org)
5. Outputs JSON or CSV with metadata + items
6. `analyze` command reads JSON, runs selected analyzers and group-by, outputs JSON or Markdown
