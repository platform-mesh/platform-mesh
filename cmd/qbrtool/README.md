# qbrtool - Quarterly Board Report Tool

A Go CLI tool for exporting and analyzing GitHub Project Board items. Supports dynamic schema discovery, custom field filtering, and pipeline-based workflows for generating quarterly reports.

## Features

- **Schema Discovery** - Discover project fields and available values dynamically
- **Dynamic Filtering** - Filter by any project field (Status, Type, Priority, etc.)
- **Full Data Export** - Export items with all custom field values, assignees, milestones
- **Cross-Org Support** - Track contributions from external organizations
- **Pipeline Workflows** - Chain commands for live data processing
- **Built-in Analyzers** - CVE, OSS, security, monitoring, lifecycle detection
- **Group-By Analysis** - Dynamically group items by any field
- **Multiple Formats** - JSON, CSV, and Markdown output

## Quick Start

### Prerequisites

- Go 1.25 or later
- GitHub CLI (`gh`) for authentication (recommended)

### Installation

```bash
git clone https://github.com/platform-mesh/platform-mesh
cd cmd/qbrtool
make build
```

### Authentication

```bash
# Using GitHub CLI (recommended)
export GITHUB_TOKEN=$(gh auth token)

# Or use a personal access token
export GITHUB_TOKEN=github_pat_xxxxxxxxxxxx
```

### Basic Pipeline

```bash
# Export items and generate a quarterly report
qbrtool export --full --quarter Q4-2025 | qbrtool analyze --analysis all --format md
```

---

## Workflow

The recommended workflow is: **Discover → Export → Analyze**

### Step 1: Discover Project Schema

Before exporting, discover what fields are available in your project.

#### Quick View (List Fields)

```bash
qbrtool export --list-fields
```

Output:
```
Available fields for platform-mesh/1 (Platform Mesh & Kube Projects - Backlog):

  Title (title)
  Status (single_select): Backlog, In Progress, Blocked/Waiting, In-Review, Done, Ongoing, Demo'd
  Priority (single_select): P0, P1, P2
  IssueType (single_select): Epic, Task
  Domain (single_select): Platform Mesh, kcp
  Milestone (milestone)
  ...

Built-in fields (always available for filtering):
  state: open, closed, merged
  type: issue, pr, draft
  repo: repository name
  author: username
  milestone: milestone title

Usage examples:
  qbrtool export --org platform-mesh --project 1 --full --field "Status=Done"
```

#### Full Schema Export

For detailed schema information (IDs, colors, iterations):

```bash
qbrtool schema -f schema.json
```

### Step 2: Export Items

Export project items with filtering options. Use `--full` to include all custom field values.

#### Basic Export (Minimal Data)

```bash
qbrtool export --quarter Q4-2025
```

#### Full Export (All Field Values)

```bash
qbrtool export --full --quarter Q4-2025
```

#### Filter by Custom Fields

```bash
# Filter by status
qbrtool export --full --field "Status=Done"

# Filter by type
qbrtool export --full --field "IssueType=Epic"

# Multiple filters (AND logic)
qbrtool export --full --field "Status=Done" --field "Priority=P0"

# Filter operators
qbrtool export --full --field "Status!=Backlog"        # Not equal
qbrtool export --full --field "Title~security"         # Contains
qbrtool export --full --field "Status=Done,In-Review"  # Match any
```

#### Filter by Organization

```bash
# Only items from your org
qbrtool export --full --org-filter internal

# Only external contributions
qbrtool export --full --org-filter external

# Specific organizations
qbrtool export --full --filter-orgs kcp-dev,kube-bind
```

#### Include Archived Items

```bash
qbrtool export --full --quarter Q4-2025 --include-archived
```

### Step 3: Analyze

Run analyzers on exported data. Pipe directly from export for live data.

#### Built-in Analyzers

```bash
# All analyzers
qbrtool export --full | qbrtool analyze --analysis all

# Specific analyzers
qbrtool export --full | qbrtool analyze --analysis cve
qbrtool export --full | qbrtool analyze --analysis oss
qbrtool export --full | qbrtool analyze --analysis security
qbrtool export --full | qbrtool analyze --analysis monitoring
qbrtool export --full | qbrtool analyze --analysis lifecycle
```

#### Group-By Analysis

Group items by any field value:

```bash
# Group by status
qbrtool export --full | qbrtool analyze --group-by Status

# Group by issue type
qbrtool export --full | qbrtool analyze --group-by IssueType

# Group by repository
qbrtool export --full | qbrtool analyze --group-by repo

# Multiple groupings
qbrtool export --full | qbrtool analyze --group-by Status --group-by IssueType
```

#### Generate Reports

```bash
# Markdown report
qbrtool export --full --quarter Q4-2025 | qbrtool analyze --analysis all --format md

# Save to file
qbrtool export --full --quarter Q4-2025 | qbrtool analyze --analysis all --format md -f report.md
```

---

## Pipeline Examples

### Quarterly Report (Recommended)

```bash
# Complete quarterly report with all analyzers and grouping
qbrtool export --full --quarter Q4-2025 --include-archived | \
  qbrtool analyze --analysis all --group-by IssueType --format md -f Q4-report.md
```

### Epic Summary

```bash
# Export only epics, analyze by status
qbrtool export --full --field "IssueType=Epic" | \
  qbrtool analyze --group-by Status --format md
```

### External Contributions Report

```bash
# Track contributions from external orgs
qbrtool export --full --org-filter external | \
  qbrtool analyze --analysis oss --group-by repo --format md
```

### Security Review

```bash
# CVE and security items
qbrtool export --full --quarter Q4-2025 | \
  qbrtool analyze --analysis cve --analysis security --format md
```

### P0 Items by Domain

```bash
# High-priority items grouped by domain
qbrtool export --full --field "Priority=P0" | \
  qbrtool analyze --group-by Domain --format md
```

---

## Command Reference

### schema

Dump the complete project schema to JSON.

```bash
qbrtool schema [flags]
```

| Flag | Short | Description | Default |
|------|-------|-------------|---------|
| `--org` | `-o` | GitHub organization | `platform-mesh` |
| `--project` | `-p` | Project number | `1` |
| `--output-file` | `-f` | Output file | stdout |

### export

Export project board items.

```bash
qbrtool export [flags]
```

| Flag | Short | Description | Default |
|------|-------|-------------|---------|
| `--org` | `-o` | GitHub organization | `platform-mesh` |
| `--project` | `-p` | Project number | `1` |
| `--quarter` | `-q` | Filter by quarter (e.g., Q4-2025) | - |
| `--type` | `-t` | Item types: issue, pr, draft, epic | all |
| `--full` | - | Include all custom field values | `false` |
| `--field` | - | Filter by field (repeatable) | - |
| `--org-filter` | - | Filter: all, internal, external | `all` |
| `--filter-orgs` | - | Specific orgs (comma-separated) | - |
| `--include-archived` | - | Include archived items | `false` |
| `--list-fields` | - | List available fields and exit | `false` |
| `--output-file` | `-f` | Output file | stdout |
| `--format` | `-F` | Format: json, csv | `json` |

**Field Filter Syntax:**
- `Field=Value` - Exact match (case-insensitive)
- `Field!=Value` - Not equal
- `Field~Value` - Contains
- `Field=A,B,C` - Match any value

### analyze

Analyze exported items.

```bash
qbrtool analyze [flags]
```

| Flag | Short | Description | Default |
|------|-------|-------------|---------|
| `--input` | `-i` | Input JSON file | stdin |
| `--analysis` | `-a` | Analyzer: all, cve, oss, security, monitoring, lifecycle | `all` |
| `--group-by` | - | Group by field (repeatable) | - |
| `--oss-orgs` | - | OSS orgs to detect | `kcp-dev,kube-bind,multicluster-runtime` |
| `--output-file` | `-f` | Output file | stdout |
| `--format` | `-F` | Format: json, markdown, md | `json` |

---

## Output Formats

### Export JSON (with --full)

```json
{
  "metadata": {
    "organization": "platform-mesh",
    "project_number": 1,
    "quarter": "Q4-2025",
    "total_items": 42
  },
  "items": [
    {
      "id": "I_xxxxx",
      "type": "ISSUE",
      "number": 123,
      "title": "Implement feature X",
      "state": "CLOSED",
      "url": "https://github.com/...",
      "closed_at": "2025-10-15T...",
      "repository": {
        "owner": "platform-mesh",
        "name": "my-repo"
      },
      "author": "username",
      "assignees": ["user1", "user2"],
      "milestone": {
        "title": "v0.8",
        "due_on": "2025-12-01T00:00:00Z"
      },
      "labels": ["enhancement", "priority/high"],
      "field_values": {
        "Status": "Done",
        "IssueType": "Task",
        "Priority": "P1",
        "Domain": "Platform Mesh"
      }
    }
  ]
}
```

### Group-By Output

```json
{
  "name": "group-by-Status",
  "groups": {
    "Done": {
      "count": 25,
      "items": [{"number": 123, "title": "...", "url": "..."}]
    },
    "In Progress": {
      "count": 10,
      "items": [{"number": 456, "title": "...", "url": "..."}]
    }
  },
  "total_items": 35
}
```

### Markdown Report

```markdown
# Quarterly Analysis Report

## Source Information

| Field | Value |
|-------|-------|
| Organization | platform-mesh |
| Project | #1 |
| Quarter | Q4-2025 |
| Total Items | 42 |

---

## Group By: Status

| Status | Count |
|--------|-------|
| Done | 25 |
| In Progress | 10 |
| Backlog | 7 |

### Done (25 items)

| # | Title | URL |
|---|-------|-----|
| 123 | Implement feature X | [#123](https://...) |
```

---

## Development

```bash
make build          # Build binary to ./bin/qbrtool
make test           # Run tests with race detection
make test-coverage  # Generate coverage report
make lint           # Run golangci-lint
make install        # Install to $GOPATH/bin
```

## Known Limitations

1. **GitHub Search API limit**: Maximum 1000 results per query. The tool splits queries by month to mitigate this.

2. **Archived items**: GitHub's ProjectV2 API doesn't return archived items directly. The tool uses a search-based workaround.

3. **Draft Issues**: Draft issues are not searchable via GitHub's search API, so they can only be retrieved from the project items query (if not archived).

4. **Rate limits**: GitHub's GraphQL API has rate limits. For large exports, you may need to wait between requests.

## License

Apache 2.0
