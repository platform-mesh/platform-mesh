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
	"time"

	"github.com/shurcooL/graphql"
	"go.platform-mesh.io/qbrtool/internal/models"
)

// GetProjectID returns the project ID for the given organization and project number
func (c *Client) GetProjectID(ctx context.Context, org string, projectNumber int) (string, error) {
	var query struct {
		Organization struct {
			ProjectV2 struct {
				ID graphql.ID
			} `graphql:"projectV2(number: $projectNumber)"`
		} `graphql:"organization(login: $org)"`
	}

	variables := map[string]interface{}{
		"org":           graphql.String(org),
		"projectNumber": graphql.Int(projectNumber),
	}

	if err := c.gql.Query(ctx, &query, variables); err != nil {
		return "", fmt.Errorf("failed to query project: %w", err)
	}

	return fmt.Sprintf("%v", query.Organization.ProjectV2.ID), nil
}

// GetProjectItems fetches all items from a project (basic mode)
func (c *Client) GetProjectItems(ctx context.Context, projectID string) ([]*models.ProjectItem, error) {
	return c.GetProjectItemsFull(ctx, projectID, false)
}

// GetProjectItemsFull fetches all items from a project with optional field values
func (c *Client) GetProjectItemsFull(ctx context.Context, projectID string, includeFieldValues bool) ([]*models.ProjectItem, error) {
	var allItems []*models.ProjectItem
	var cursor *graphql.String

	for {
		var query struct {
			Node struct {
				ProjectV2 struct {
					Items struct {
						TotalCount graphql.Int
						PageInfo   struct {
							HasNextPage graphql.Boolean
							EndCursor   graphql.String
						}
						Nodes []struct {
							ID          graphql.ID
							IsArchived  graphql.Boolean
							Type        graphql.String
							CreatedAt   graphql.String
							UpdatedAt   graphql.String
							FieldValues struct {
								Nodes []struct {
									Typename graphql.String `graphql:"__typename"`
									// Text field value
									TextField struct {
										Field struct {
											Common struct {
												Name graphql.String
											} `graphql:"... on ProjectV2Field"`
										}
										Text graphql.String
									} `graphql:"... on ProjectV2ItemFieldTextValue"`
									// Number field value
									NumberField struct {
										Field struct {
											Common struct {
												Name graphql.String
											} `graphql:"... on ProjectV2Field"`
										}
										Number graphql.Float
									} `graphql:"... on ProjectV2ItemFieldNumberValue"`
									// Date field value
									DateField struct {
										Field struct {
											Common struct {
												Name graphql.String
											} `graphql:"... on ProjectV2Field"`
										}
										Date graphql.String
									} `graphql:"... on ProjectV2ItemFieldDateValue"`
									// Single select field value
									SingleSelectField struct {
										Field struct {
											SSField struct {
												Name graphql.String
											} `graphql:"... on ProjectV2SingleSelectField"`
										}
										Name graphql.String
									} `graphql:"... on ProjectV2ItemFieldSingleSelectValue"`
									// Iteration field value
									IterationField struct {
										Field struct {
											IterField struct {
												Name graphql.String
											} `graphql:"... on ProjectV2IterationField"`
										}
										Title     graphql.String
										StartDate graphql.String
										Duration  graphql.Int
									} `graphql:"... on ProjectV2ItemFieldIterationValue"`
								}
							} `graphql:"fieldValues(first: 50)"`
							Content struct {
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
									Author    struct {
										Login graphql.String
									}
									Assignees struct {
										Nodes []struct {
											Login graphql.String
										}
									} `graphql:"assignees(first: 20)"`
									Milestone struct {
										Title graphql.String
										DueOn graphql.String
									}
									Labels struct {
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
								} `graphql:"... on Issue"`
								PullRequest struct {
									ID           graphql.ID
									Number       graphql.Int
									Title        graphql.String
									Body         graphql.String
									State        graphql.String
									URL          graphql.String `graphql:"url"`
									CreatedAt    graphql.String
									ClosedAt     graphql.String
									MergedAt     graphql.String
									Additions    graphql.Int
									Deletions    graphql.Int
									ChangedFiles graphql.Int
									Author       struct {
										Login graphql.String
									}
									Assignees struct {
										Nodes []struct {
											Login graphql.String
										}
									} `graphql:"assignees(first: 20)"`
									Milestone struct {
										Title graphql.String
										DueOn graphql.String
									}
									Labels struct {
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
								} `graphql:"... on PullRequest"`
								DraftIssue struct {
									Title     graphql.String
									Body      graphql.String
									CreatedAt graphql.String
									Creator   struct {
										Login graphql.String
									}
								} `graphql:"... on DraftIssue"`
							}
						}
					} `graphql:"items(first: 100, after: $cursor)"`
				} `graphql:"... on ProjectV2"`
			} `graphql:"node(id: $projectId)"`
		}

		variables := map[string]interface{}{
			"projectId": graphql.ID(projectID),
			"cursor":    cursor,
		}

		if err := c.gql.Query(ctx, &query, variables); err != nil {
			return nil, fmt.Errorf("failed to query project items: %w", err)
		}

		// Convert nodes to ProjectItem models
		for _, node := range query.Node.ProjectV2.Items.Nodes {
			item := &models.ProjectItem{
				ProjectItemID: fmt.Sprintf("%v", node.ID),
				IsArchived:    bool(node.IsArchived),
				FieldValues:   make(map[string]string),
			}

			// Parse content based on type
			switch string(node.Content.Typename) {
			case "Issue":
				issue := node.Content.Issue
				item.ID = fmt.Sprintf("%v", issue.ID)
				item.Type = models.ItemTypeIssue
				item.Number = int(issue.Number)
				item.Title = string(issue.Title)
				item.Body = string(issue.Body)
				item.State = string(issue.State)
				item.URL = string(issue.URL)
				item.CreatedAt = parseTime(string(issue.CreatedAt))
				item.ClosedAt = parseTimePtr(string(issue.ClosedAt))
				item.Repository = models.Repository{
					Owner: string(issue.Repository.Owner.Login),
					Name:  string(issue.Repository.Name),
				}
				item.Author = string(issue.Author.Login)
				for _, assignee := range issue.Assignees.Nodes {
					item.Assignees = append(item.Assignees, string(assignee.Login))
				}
				if string(issue.Milestone.Title) != "" {
					item.Milestone = &models.Milestone{
						Title: string(issue.Milestone.Title),
						DueOn: parseTimePtr(string(issue.Milestone.DueOn)),
					}
				}
				for _, label := range issue.Labels.Nodes {
					item.Labels = append(item.Labels, string(label.Name))
				}

			case "PullRequest":
				pr := node.Content.PullRequest
				item.ID = fmt.Sprintf("%v", pr.ID)
				item.Type = models.ItemTypePullRequest
				item.Number = int(pr.Number)
				item.Title = string(pr.Title)
				item.Body = string(pr.Body)
				item.State = string(pr.State)
				item.URL = string(pr.URL)
				item.CreatedAt = parseTime(string(pr.CreatedAt))
				item.ClosedAt = parseTimePtr(string(pr.ClosedAt))
				item.MergedAt = parseTimePtr(string(pr.MergedAt))
				item.Repository = models.Repository{
					Owner: string(pr.Repository.Owner.Login),
					Name:  string(pr.Repository.Name),
				}
				item.Author = string(pr.Author.Login)
				for _, assignee := range pr.Assignees.Nodes {
					item.Assignees = append(item.Assignees, string(assignee.Login))
				}
				if string(pr.Milestone.Title) != "" {
					item.Milestone = &models.Milestone{
						Title: string(pr.Milestone.Title),
						DueOn: parseTimePtr(string(pr.Milestone.DueOn)),
					}
				}
				item.PRStats = &models.PRStats{
					Additions:    int(pr.Additions),
					Deletions:    int(pr.Deletions),
					ChangedFiles: int(pr.ChangedFiles),
				}
				for _, label := range pr.Labels.Nodes {
					item.Labels = append(item.Labels, string(label.Name))
				}

			case "DraftIssue":
				draft := node.Content.DraftIssue
				item.ID = item.ProjectItemID
				item.Type = models.ItemTypeDraftIssue
				item.Title = string(draft.Title)
				item.Body = string(draft.Body)
				item.CreatedAt = parseTime(string(draft.CreatedAt))
				item.Author = string(draft.Creator.Login)

			default:
				continue
			}

			if string(node.UpdatedAt) != "" {
				item.UpdatedAt = parseTime(string(node.UpdatedAt))
			}

			// Extract field values if requested
			if includeFieldValues {
				for _, fv := range node.FieldValues.Nodes {
					extractFieldValue(item.FieldValues, fv)
				}
			}

			allItems = append(allItems, item)
		}

		// Check for more pages
		if !bool(query.Node.ProjectV2.Items.PageInfo.HasNextPage) {
			break
		}
		cursor = &query.Node.ProjectV2.Items.PageInfo.EndCursor
	}

	return allItems, nil
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func parseTimePtr(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

// extractFieldValue extracts a field value and adds it to the map
func extractFieldValue(fieldValues map[string]string, fv struct {
	Typename  graphql.String `graphql:"__typename"`
	TextField struct {
		Field struct {
			Common struct {
				Name graphql.String
			} `graphql:"... on ProjectV2Field"`
		}
		Text graphql.String
	} `graphql:"... on ProjectV2ItemFieldTextValue"`
	NumberField struct {
		Field struct {
			Common struct {
				Name graphql.String
			} `graphql:"... on ProjectV2Field"`
		}
		Number graphql.Float
	} `graphql:"... on ProjectV2ItemFieldNumberValue"`
	DateField struct {
		Field struct {
			Common struct {
				Name graphql.String
			} `graphql:"... on ProjectV2Field"`
		}
		Date graphql.String
	} `graphql:"... on ProjectV2ItemFieldDateValue"`
	SingleSelectField struct {
		Field struct {
			SSField struct {
				Name graphql.String
			} `graphql:"... on ProjectV2SingleSelectField"`
		}
		Name graphql.String
	} `graphql:"... on ProjectV2ItemFieldSingleSelectValue"`
	IterationField struct {
		Field struct {
			IterField struct {
				Name graphql.String
			} `graphql:"... on ProjectV2IterationField"`
		}
		Title     graphql.String
		StartDate graphql.String
		Duration  graphql.Int
	} `graphql:"... on ProjectV2ItemFieldIterationValue"`
}) {
	typename := string(fv.Typename)

	switch typename {
	case "ProjectV2ItemFieldTextValue":
		name := string(fv.TextField.Field.Common.Name)
		if name != "" && string(fv.TextField.Text) != "" {
			fieldValues[name] = string(fv.TextField.Text)
		}
	case "ProjectV2ItemFieldNumberValue":
		name := string(fv.NumberField.Field.Common.Name)
		if name != "" {
			fieldValues[name] = fmt.Sprintf("%.0f", fv.NumberField.Number)
		}
	case "ProjectV2ItemFieldDateValue":
		name := string(fv.DateField.Field.Common.Name)
		if name != "" && string(fv.DateField.Date) != "" {
			fieldValues[name] = string(fv.DateField.Date)
		}
	case "ProjectV2ItemFieldSingleSelectValue":
		name := string(fv.SingleSelectField.Field.SSField.Name)
		if name != "" && string(fv.SingleSelectField.Name) != "" {
			fieldValues[name] = string(fv.SingleSelectField.Name)
		}
	case "ProjectV2ItemFieldIterationValue":
		name := string(fv.IterationField.Field.IterField.Name)
		if name != "" && string(fv.IterationField.Title) != "" {
			fieldValues[name] = string(fv.IterationField.Title)
		}
	}
}
