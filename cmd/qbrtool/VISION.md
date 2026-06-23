# VISION.md

## Overview

**qbrtool** is a data extraction and analysis pipeline for GitHub Project Boards, designed to support Quarterly Business Review (QBR) reporting. It is intentionally **not** a complete reporting solution—it serves as a structured data layer that feeds into LLMs, MCP servers, or other tools for final report generation.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           QBR Reporting Pipeline                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                 │
│   │   GitHub     │    │   qbrtool    │    │  LLM / MCP   │                 │
│   │  Project     │───▶│   Extract    │───▶│   Format &   │───▶  QBR Report │
│   │   Board      │    │   & Analyze  │    │   Present    │                 │
│   └──────────────┘    └──────────────┘    └──────────────┘                 │
│                                                                             │
│        Source              Pipeline            Presentation                 │
│        of Truth            (this tool)         (external)                   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Design Philosophy

### 1. Schema-Agnostic Architecture

qbrtool has **no hardcoded field dependencies**. It dynamically discovers project schemas and works with any GitHub ProjectV2 configuration.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Schema Discovery Flow                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   qbrtool schema                    qbrtool export --list-fields            │
│        │                                   │                                │
│        ▼                                   ▼                                │
│   ┌─────────────┐                   ┌─────────────┐                        │
│   │  Fetch All  │                   │   Display   │                        │
│   │   Fields    │                   │  Available  │                        │
│   │  & Types    │                   │   Filters   │                        │
│   └──────┬──────┘                   └──────┬──────┘                        │
│          │                                 │                                │
│          ▼                                 ▼                                │
│   ┌─────────────────────────────────────────────────────────────────┐      │
│   │                     schema.json                                  │      │
│   │  {                                                               │      │
│   │    "fields": [                                                   │      │
│   │      { "name": "Status", "type": "SINGLE_SELECT",               │      │
│   │        "options": ["Backlog", "In Progress", "Done"] },         │      │
│   │      { "name": "Priority", "type": "SINGLE_SELECT",             │      │
│   │        "options": ["P0", "P1", "P2"] },                         │      │
│   │      { "name": "IssueType", "type": "SINGLE_SELECT",            │      │
│   │        "options": ["Epic", "Task"] },                           │      │
│   │      { "name": "Sprint", "type": "ITERATION" },                 │      │
│   │      ...                                                         │      │
│   │    ]                                                             │      │
│   │  }                                                               │      │
│   └─────────────────────────────────────────────────────────────────┘      │
│                                                                             │
│   Any field can be used for filtering: --field "YourCustomField=Value"     │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2. Data Extraction Focus

The tool prioritizes **structured data output** over presentation. All output formats (JSON, CSV, Markdown) are designed for machine consumption or further processing.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Data Flow Architecture                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│                        ┌─────────────────────┐                              │
│                        │   GitHub GraphQL    │                              │
│                        │        API          │                              │
│                        └──────────┬──────────┘                              │
│                                   │                                         │
│              ┌────────────────────┼────────────────────┐                    │
│              │                    │                    │                    │
│              ▼                    ▼                    ▼                    │
│     ┌────────────────┐  ┌────────────────┐  ┌────────────────┐             │
│     │ Project Items  │  │ Search API     │  │ Schema         │             │
│     │ (active)       │  │ (archived)     │  │ (fields)       │             │
│     └───────┬────────┘  └───────┬────────┘  └───────┬────────┘             │
│             │                   │                   │                       │
│             └─────────┬─────────┘                   │                       │
│                       ▼                             │                       │
│              ┌────────────────┐                     │                       │
│              │  Filter Chain  │◀────────────────────┘                       │
│              │  (dynamic)     │   (schema informs valid filters)            │
│              └───────┬────────┘                                             │
│                      │                                                      │
│         ┌────────────┼────────────┐                                         │
│         ▼            ▼            ▼                                         │
│   ┌──────────┐ ┌──────────┐ ┌──────────┐                                   │
│   │   JSON   │ │   CSV    │ │ Markdown │                                   │
│   │  export  │ │  export  │ │  export  │                                   │
│   └────┬─────┘ └────┬─────┘ └────┬─────┘                                   │
│        │            │            │                                          │
│        └────────────┼────────────┘                                          │
│                     ▼                                                       │
│            ┌────────────────┐                                               │
│            │   Analyzers    │                                               │
│            │  (optional)    │                                               │
│            └───────┬────────┘                                               │
│                    │                                                        │
│    ┌───────────────┼───────────────┬───────────────┐                        │
│    ▼               ▼               ▼               ▼                        │
│ ┌──────┐      ┌──────┐       ┌──────────┐    ┌──────────┐                  │
│ │ CVE  │      │ OSS  │       │ Group-By │    │ Security │                  │
│ └──────┘      └──────┘       └──────────┘    └──────────┘                  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 3. Pipeline-First Design

qbrtool commands are designed to chain via stdin/stdout, enabling flexible workflows.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Pipeline Patterns                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Pattern 1: Direct Export                                                   │
│  ────────────────────────                                                   │
│                                                                             │
│    qbrtool export --full --quarter Q4-2025 > data.json                     │
│                                                                             │
│                                                                             │
│  Pattern 2: Export + Analyze Pipeline                                       │
│  ───────────────────────────────────────                                    │
│                                                                             │
│    qbrtool export ──┬──▶ qbrtool analyze ──▶ report.md                     │
│         │          │           │                                            │
│         │          │           └── --analysis all --group-by Status        │
│         │          │                                                        │
│         │          └── stdin (JSON)                                         │
│         │                                                                   │
│         └── --full --quarter Q4-2025                                       │
│                                                                             │
│                                                                             │
│  Pattern 3: Export + LLM Processing                                         │
│  ──────────────────────────────────                                         │
│                                                                             │
│    qbrtool export ──▶ qbrtool analyze ──▶ LLM/MCP ──▶ Final Report         │
│                              │                │                             │
│                              │                └── Natural language          │
│                              │                    formatting, insights      │
│                              │                                              │
│                              └── Structured data with                       │
│                                  analysis results                           │
│                                                                             │
│                                                                             │
│  Pattern 4: Multi-Analysis                                                  │
│  ────────────────────────                                                   │
│                                                                             │
│    qbrtool export --full > data.json                                       │
│                                                                             │
│    cat data.json | qbrtool analyze --analysis cve     > cve-report.json    │
│    cat data.json | qbrtool analyze --group-by Status  > status.json        │
│    cat data.json | qbrtool analyze --group-by IssueType > types.json       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## QBR Report Coverage

qbrtool is designed to extract evidence for standard QBR criteria:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         QBR Evidence Matrix                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  QBR Criteria              │  qbrtool Extraction Method                     │
│  ─────────────────────────────────────────────────────────────────────────  │
│                            │                                                │
│  Major Milestones          │  --field "IssueType=Epic" --field "Status=Done"│
│                            │  Items with milestone field populated          │
│                            │  --group-by Milestone                          │
│                            │                                                │
│  ─────────────────────────────────────────────────────────────────────────  │
│                            │                                                │
│  Large Features (Epics)    │  --field "IssueType=Epic"                      │
│                            │  --group-by Status (to show progress)          │
│                            │                                                │
│  ─────────────────────────────────────────────────────────────────────────  │
│                            │                                                │
│  Completed Work            │  --quarter Q4-2025 --field "Status=Done"       │
│  (Evidence)                │  Closed issues/PRs in quarter                  │
│                            │  --group-by IssueType for breakdown            │
│                            │                                                │
│  ─────────────────────────────────────────────────────────────────────────  │
│                            │                                                │
│  Security/CVE Work         │  --analysis cve                                │
│                            │  --analysis security                           │
│                            │                                                │
│  ─────────────────────────────────────────────────────────────────────────  │
│                            │                                                │
│  OSS Contributions         │  --org-filter external                         │
│                            │  --analysis oss                                │
│                            │  --filter-orgs kcp-dev,kube-bind               │
│                            │                                                │
│  ─────────────────────────────────────────────────────────────────────────  │
│                            │                                                │
│  Cross-Team Dependencies   │  --group-by Domain                             │
│                            │  --group-by repo                               │
│                            │                                                │
│  ─────────────────────────────────────────────────────────────────────────  │
│                            │                                                │
│  Priority Distribution     │  --group-by Priority                           │
│                            │  --field "Priority=P0" for critical items      │
│                            │                                                │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Integration with LLMs / MCP

qbrtool is designed as the **data layer** in an LLM-assisted reporting workflow:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        LLM Integration Architecture                         │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                        MCP Server (Future)                          │   │
│   │                                                                     │   │
│   │   Tools exposed to LLM:                                             │   │
│   │   ┌─────────────────────────────────────────────────────────────┐   │   │
│   │   │  qbr_schema      - Get project field definitions            │   │   │
│   │   │  qbr_export      - Export items with filters                │   │   │
│   │   │  qbr_analyze     - Run analysis on exported data            │   │   │
│   │   │  qbr_list_fields - List available filter fields             │   │   │
│   │   └─────────────────────────────────────────────────────────────┘   │   │
│   │                                                                     │   │
│   └──────────────────────────────┬──────────────────────────────────────┘   │
│                                  │                                          │
│                                  ▼                                          │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                           LLM Agent                                 │   │
│   │                                                                     │   │
│   │   1. "What fields are available?" ──▶ qbr_list_fields              │   │
│   │                                                                     │   │
│   │   2. "Get Q4 completed epics" ──▶ qbr_export                       │   │
│   │       --quarter Q4-2025 --field "IssueType=Epic"                   │   │
│   │       --field "Status=Done"                                         │   │
│   │                                                                     │   │
│   │   3. "Analyze by priority" ──▶ qbr_analyze                         │   │
│   │       --group-by Priority                                           │   │
│   │                                                                     │   │
│   │   4. Format results into executive summary                          │   │
│   │                                                                     │   │
│   └──────────────────────────────┬──────────────────────────────────────┘   │
│                                  │                                          │
│                                  ▼                                          │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                      Final QBR Report                               │   │
│   │                                                                     │   │
│   │   - Executive summary (LLM-generated)                               │   │
│   │   - Key achievements with evidence (from qbrtool data)              │   │
│   │   - Metrics and breakdowns (from analyzers)                         │   │
│   │   - Risk items and blockers (filtered items)                        │   │
│   │   - Next quarter outlook (LLM-generated)                            │   │
│   │                                                                     │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Current Usage Pattern (without MCP)

```bash
# Step 1: Extract structured data
qbrtool export --full --quarter Q4-2025 --include-archived > q4-data.json

# Step 2: Run analyses
qbrtool analyze -i q4-data.json --analysis all --group-by IssueType --format md > analysis.md

# Step 3: Feed to LLM for formatting
cat analysis.md | llm "Format this as an executive QBR summary with highlights"

# Or use with Claude Code / Cursor / etc:
# "Here is my Q4 data from qbrtool: @q4-data.json @analysis.md
#  Please create a QBR report highlighting major achievements..."
```

## Component Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Internal Architecture                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  cmd/qbrtool/main.go                                                        │
│         │                                                                   │
│         ▼                                                                   │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     internal/cli/                                    │   │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐            │   │
│  │  │ root.go  │  │schema.go │  │export.go │  │analyze.go│            │   │
│  │  │ (flags)  │  │          │  │          │  │          │            │   │
│  │  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘            │   │
│  └───────┼─────────────┼─────────────┼─────────────┼────────────────────┘   │
│          │             │             │             │                        │
│          ▼             ▼             ▼             ▼                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                   internal/github/                                   │   │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐        │   │
│  │  │ client.go │  │project.go │  │ search.go │  │ schema.go │        │   │
│  │  │ (GraphQL) │  │ (items)   │  │ (archive) │  │ (fields)  │        │   │
│  │  └───────────┘  └───────────┘  └───────────┘  └───────────┘        │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                    │                                        │
│          ┌─────────────────────────┼─────────────────────────┐              │
│          ▼                         ▼                         ▼              │
│  ┌───────────────┐        ┌───────────────┐        ┌───────────────┐       │
│  │internal/models│        │internal/filter│        │internal/       │       │
│  │               │        │               │        │  analyzer      │       │
│  │ - item.go     │        │ - filter.go   │        │               │       │
│  │ - quarter.go  │        │ - time.go     │        │ - analyzer.go │       │
│  │ - schema.go   │        │ - type.go     │        │ - cve.go      │       │
│  │               │        │ - dynamic.go  │        │ - oss.go      │       │
│  │               │        │               │        │ - dynamic.go  │       │
│  │               │        │               │        │ - markdown.go │       │
│  └───────────────┘        └───────────────┘        └───────────────┘       │
│                                                            │                │
│                                                            ▼                │
│                                               ┌───────────────────┐         │
│                                               │internal/exporter  │         │
│                                               │                   │         │
│                                               │ - json.go         │         │
│                                               │ - csv.go          │         │
│                                               └───────────────────┘         │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Example QBR Workflow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      Complete QBR Generation Workflow                       │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Step 1: Discovery                                                          │
│  ─────────────────                                                          │
│                                                                             │
│    $ qbrtool export --list-fields                                          │
│                                                                             │
│    Available fields:                                                        │
│      Status (single_select): Backlog, In Progress, Done...                 │
│      IssueType (single_select): Epic, Task                                 │
│      Priority (single_select): P0, P1, P2                                  │
│      Domain (single_select): Platform Mesh, kcp                            │
│                                                                             │
│                                                                             │
│  Step 2: Export Quarter Data                                                │
│  ───────────────────────────                                                │
│                                                                             │
│    $ qbrtool export --full --quarter Q4-2025 \                             │
│        --include-archived -f q4-raw.json                                   │
│                                                                             │
│                                                                             │
│  Step 3: Generate Analysis Reports                                          │
│  ─────────────────────────────────                                          │
│                                                                             │
│    # Overall summary                                                        │
│    $ qbrtool analyze -i q4-raw.json \                                      │
│        --analysis all \                                                     │
│        --group-by IssueType \                                              │
│        --group-by Status \                                                 │
│        --format md -f q4-analysis.md                                       │
│                                                                             │
│    # Epic progress                                                          │
│    $ qbrtool export --full --field "IssueType=Epic" | \                    │
│        qbrtool analyze --group-by Status --format md -f epics.md           │
│                                                                             │
│    # Security/CVE work                                                      │
│    $ qbrtool analyze -i q4-raw.json \                                      │
│        --analysis cve --analysis security \                                │
│        --format md -f security.md                                          │
│                                                                             │
│    # OSS contributions                                                      │
│    $ qbrtool export --full --org-filter external | \                       │
│        qbrtool analyze --analysis oss --format md -f oss.md                │
│                                                                             │
│                                                                             │
│  Step 4: LLM Formatting (External)                                          │
│  ─────────────────────────────────                                          │
│                                                                             │
│    Feed q4-analysis.md, epics.md, security.md, oss.md to LLM:              │
│                                                                             │
│    "Using these analysis reports, create a QBR document with:              │
│     - Executive Summary                                                     │
│     - Key Achievements (completed epics, milestones)                       │
│     - Security Posture (CVEs addressed)                                    │
│     - Open Source Impact                                                   │
│     - Risks and Blockers                                                   │
│     - Next Quarter Priorities"                                             │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Non-Goals

qbrtool intentionally does **NOT**:

- Generate polished prose or executive summaries (use LLM)
- Create charts or visualizations (use external tools)
- Store historical data or trends (export fresh each time)
- Manage or update project items (read-only)
- Handle authentication flows (expects GITHUB_TOKEN)
- Format for specific document templates (outputs structured data)

## Future Considerations

1. **MCP Server Implementation** - Expose qbrtool as MCP tools for direct LLM integration
2. **Caching Layer** - Optional caching for expensive GraphQL queries
3. **Template System** - Pluggable output templates for common report formats
4. **Diff/Trend Analysis** - Compare exports across quarters
5. **Multi-Project Support** - Aggregate across multiple projects in single export
