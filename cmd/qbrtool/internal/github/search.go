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

package github

import (
	"context"
	"fmt"
	"os"

	"github.com/shurcooL/graphql"
	"go.platform-mesh.io/qbrtool/internal/models"
)

// SearchArchivedItems searches for issues/PRs that are archived in the project
// It searches across all repos that have items in the project
func (c *Client) SearchArchivedItems(ctx context.Context, org string, projectNumber int, quarter *models.Quarter, currentItems []*models.ProjectItem, verbose bool) ([]*models.ProjectItem, error) {
	var allItems []*models.ProjectItem
	seen := make(map[string]bool)

	// Extract unique repos from current items
	repos := extractRepos(currentItems)

	// Always include the org's repos as well
	repos = append(repos, org)

	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] Searching in repos: %v\n", repos)
	}

	// Build search queries for each repo
	queries := buildRepoSearchQueries(repos, quarter)

	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] Running %d search queries\n", len(queries))
	}

	for i, searchQuery := range queries {
		if verbose && i < 5 {
			fmt.Fprintf(os.Stderr, "[DEBUG] Query %d: %s\n", i+1, searchQuery)
		}
		items, err := c.searchWithProjectItems(ctx, searchQuery, projectNumber)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "[DEBUG] Query failed: %v\n", err)
			}
			continue
		}
		if verbose && len(items) > 0 {
			fmt.Fprintf(os.Stderr, "[DEBUG] Query %d found %d archived items\n", i+1, len(items))
		}

		// Deduplicate
		for _, item := range items {
			if !seen[item.ID] {
				seen[item.ID] = true
				allItems = append(allItems, item)
			}
		}
	}

	return allItems, nil
}

// extractRepos gets unique repos from project items
func extractRepos(items []*models.ProjectItem) []string {
	repoSet := make(map[string]bool)
	for _, item := range items {
		if item.Repository.Owner != "" && item.Repository.Name != "" {
			repo := item.Repository.Owner + "/" + item.Repository.Name
			repoSet[repo] = true
		}
	}

	repos := make([]string, 0, len(repoSet))
	for repo := range repoSet {
		repos = append(repos, repo)
	}
	return repos
}

// buildRepoSearchQueries builds search queries for specific repos
func buildRepoSearchQueries(repos []string, quarter *models.Quarter) []string {
	var queries []string

	for _, repo := range repos {
		if quarter == nil {
			queries = append(queries, fmt.Sprintf("repo:%s is:issue", repo))
			queries = append(queries, fmt.Sprintf("repo:%s is:pr", repo))
		} else {
			// Split by month within the quarter
			start := quarter.StartDate()
			for month := 0; month < 3; month++ {
				monthStart := start.AddDate(0, month, 0)
				monthEnd := monthStart.AddDate(0, 1, -1)

				dateRange := fmt.Sprintf("%s..%s",
					monthStart.Format("2006-01-02"),
					monthEnd.Format("2006-01-02"))

				// Issues and PRs created or closed in this month
				queries = append(queries, fmt.Sprintf("repo:%s is:issue created:%s", repo, dateRange))
				queries = append(queries, fmt.Sprintf("repo:%s is:pr created:%s", repo, dateRange))
				queries = append(queries, fmt.Sprintf("repo:%s is:issue closed:%s", repo, dateRange))
				queries = append(queries, fmt.Sprintf("repo:%s is:pr closed:%s", repo, dateRange))
			}
		}
	}

	return queries
}

// searchWithProjectItems searches for issues/PRs and checks their projectItems connection
func (c *Client) searchWithProjectItems(ctx context.Context, searchQuery string, targetProjectNumber int) ([]*models.ProjectItem, error) {
	var allItems []*models.ProjectItem
	var cursor *graphql.String

	for {
		var query struct {
			Search struct {
				IssueCount graphql.Int
				PageInfo   struct {
					HasNextPage graphql.Boolean
					EndCursor   graphql.String
				}
				Nodes []struct {
					Typename graphql.String `graphql:"__typename"`
					Issue    struct {
						ID        graphql.ID
						Number    graphql.Int
						Title     graphql.String
						Body      graphql.String
						State     graphql.String
						URL       graphql.String `graphql:"url"`
						CreatedAt graphql.String
						ClosedAt  graphql.String
						Labels    struct {
							Nodes []struct {
								Name graphql.String
							}
						} `graphql:"labels(first: 20)"`
						Repository struct {
							Owner struct {
								Login graphql.String
							}
							Name graphql.String
						}
						ProjectItems struct {
							Nodes []struct {
								ID         graphql.ID
								IsArchived graphql.Boolean
								Project    struct {
									Number graphql.Int
								}
							}
						} `graphql:"projectItems(first: 20)"`
					} `graphql:"... on Issue"`
					PullRequest struct {
						ID        graphql.ID
						Number    graphql.Int
						Title     graphql.String
						Body      graphql.String
						State     graphql.String
						URL       graphql.String `graphql:"url"`
						CreatedAt graphql.String
						ClosedAt  graphql.String
						MergedAt  graphql.String
						Labels    struct {
							Nodes []struct {
								Name graphql.String
							}
						} `graphql:"labels(first: 20)"`
						Repository struct {
							Owner struct {
								Login graphql.String
							}
							Name graphql.String
						}
						ProjectItems struct {
							Nodes []struct {
								ID         graphql.ID
								IsArchived graphql.Boolean
								Project    struct {
									Number graphql.Int
								}
							}
						} `graphql:"projectItems(first: 20)"`
					} `graphql:"... on PullRequest"`
				}
			} `graphql:"search(query: $searchQuery, type: ISSUE, first: 100, after: $cursor)"`
		}

		variables := map[string]interface{}{
			"searchQuery": graphql.String(searchQuery),
			"cursor":      cursor,
		}

		if err := c.gql.Query(ctx, &query, variables); err != nil {
			return nil, fmt.Errorf("search query failed: %w", err)
		}

		// Process results
		for _, node := range query.Search.Nodes {
			switch string(node.Typename) {
			case "Issue":
				issue := node.Issue
				for _, pi := range issue.ProjectItems.Nodes {
					if int(pi.Project.Number) == targetProjectNumber && bool(pi.IsArchived) {
						item := &models.ProjectItem{
							ID:            fmt.Sprintf("%v", issue.ID),
							ProjectItemID: fmt.Sprintf("%v", pi.ID),
							Type:          models.ItemTypeIssue,
							IsArchived:    true,
							Number:        int(issue.Number),
							Title:         string(issue.Title),
							Body:          string(issue.Body),
							State:         string(issue.State),
							URL:           string(issue.URL),
							CreatedAt:     parseTime(string(issue.CreatedAt)),
							ClosedAt:      parseTimePtr(string(issue.ClosedAt)),
							Repository: models.Repository{
								Owner: string(issue.Repository.Owner.Login),
								Name:  string(issue.Repository.Name),
							},
							FieldValues: make(map[string]string),
						}
						for _, label := range issue.Labels.Nodes {
							item.Labels = append(item.Labels, string(label.Name))
						}
						allItems = append(allItems, item)
						break
					}
				}

			case "PullRequest":
				pr := node.PullRequest
				for _, pi := range pr.ProjectItems.Nodes {
					if int(pi.Project.Number) == targetProjectNumber && bool(pi.IsArchived) {
						item := &models.ProjectItem{
							ID:            fmt.Sprintf("%v", pr.ID),
							ProjectItemID: fmt.Sprintf("%v", pi.ID),
							Type:          models.ItemTypePullRequest,
							IsArchived:    true,
							Number:        int(pr.Number),
							Title:         string(pr.Title),
							Body:          string(pr.Body),
							State:         string(pr.State),
							URL:           string(pr.URL),
							CreatedAt:     parseTime(string(pr.CreatedAt)),
							ClosedAt:      parseTimePtr(string(pr.ClosedAt)),
							MergedAt:      parseTimePtr(string(pr.MergedAt)),
							Repository: models.Repository{
								Owner: string(pr.Repository.Owner.Login),
								Name:  string(pr.Repository.Name),
							},
							FieldValues: make(map[string]string),
						}
						for _, label := range pr.Labels.Nodes {
							item.Labels = append(item.Labels, string(label.Name))
						}
						allItems = append(allItems, item)
						break
					}
				}
			}
		}

		// Check for more pages
		if !bool(query.Search.PageInfo.HasNextPage) {
			break
		}
		cursor = &query.Search.PageInfo.EndCursor
	}

	return allItems, nil
}
