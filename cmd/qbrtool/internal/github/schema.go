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

// GetProjectSchema fetches the complete schema for a project
func (c *Client) GetProjectSchema(ctx context.Context, org string, projectNumber int) (*models.ProjectSchema, error) {
	// First get project ID and basic info
	var projectQuery struct {
		Organization struct {
			ProjectV2 struct {
				ID               graphql.String
				Title            graphql.String
				ShortDescription graphql.String
				Public           graphql.Boolean
				URL              graphql.String `graphql:"url"`
			} `graphql:"projectV2(number: $projectNumber)"`
		} `graphql:"organization(login: $org)"`
	}

	variables := map[string]interface{}{
		"org":           graphql.String(org),
		"projectNumber": graphql.Int(projectNumber),
	}

	if err := c.gql.Query(ctx, &projectQuery, variables); err != nil {
		return nil, fmt.Errorf("failed to fetch project info: %w", err)
	}

	projectID := string(projectQuery.Organization.ProjectV2.ID)

	// Now fetch all fields with their configurations
	fields, err := c.getProjectFields(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch project fields: %w", err)
	}

	return &models.ProjectSchema{
		Project: models.ProjectInfo{
			ID:           projectID,
			Title:        string(projectQuery.Organization.ProjectV2.Title),
			Organization: org,
			Number:       projectNumber,
			Description:  string(projectQuery.Organization.ProjectV2.ShortDescription),
			Public:       bool(projectQuery.Organization.ProjectV2.Public),
			URL:          string(projectQuery.Organization.ProjectV2.URL),
		},
		Fields:    fields,
		FetchedAt: time.Now(),
	}, nil
}

// getProjectFields fetches all field definitions for a project
func (c *Client) getProjectFields(ctx context.Context, projectID string) ([]models.FieldSchema, error) {
	var query struct {
		Node struct {
			ProjectV2 struct {
				Fields struct {
					PageInfo struct {
						HasNextPage graphql.Boolean
						EndCursor   graphql.String
					}
					Nodes []struct {
						Typename graphql.String `graphql:"__typename"`

						// ProjectV2Field (basic fields)
						ProjectV2Field struct {
							ID       graphql.String
							Name     graphql.String
							DataType graphql.String
						} `graphql:"... on ProjectV2Field"`

						// ProjectV2SingleSelectField
						ProjectV2SingleSelectField struct {
							ID      graphql.String
							Name    graphql.String
							Options []struct {
								ID          graphql.String
								Name        graphql.String
								Description graphql.String
								Color       graphql.String
							}
						} `graphql:"... on ProjectV2SingleSelectField"`

						// ProjectV2IterationField
						ProjectV2IterationField struct {
							ID            graphql.String
							Name          graphql.String
							Configuration struct {
								Iterations []struct {
									ID        graphql.String
									Title     graphql.String
									StartDate graphql.String
									Duration  graphql.Int
								}
							}
						} `graphql:"... on ProjectV2IterationField"`
					}
				} `graphql:"fields(first: 100, after: $cursor)"`
			} `graphql:"... on ProjectV2"`
		} `graphql:"node(id: $projectId)"`
	}

	var allFields []models.FieldSchema
	var cursor *graphql.String

	for {
		variables := map[string]interface{}{
			"projectId": graphql.ID(projectID),
			"cursor":    cursor,
		}

		if err := c.gql.Query(ctx, &query, variables); err != nil {
			return nil, fmt.Errorf("failed to fetch fields: %w", err)
		}

		for _, node := range query.Node.ProjectV2.Fields.Nodes {
			field := convertFieldNode(node)
			if field != nil {
				allFields = append(allFields, *field)
			}
		}

		if !query.Node.ProjectV2.Fields.PageInfo.HasNextPage {
			break
		}
		cursor = &query.Node.ProjectV2.Fields.PageInfo.EndCursor
	}

	return allFields, nil
}

type fieldNode struct {
	Typename graphql.String `graphql:"__typename"`

	ProjectV2Field struct {
		ID       graphql.String
		Name     graphql.String
		DataType graphql.String
	} `graphql:"... on ProjectV2Field"`

	ProjectV2SingleSelectField struct {
		ID      graphql.String
		Name    graphql.String
		Options []struct {
			ID          graphql.String
			Name        graphql.String
			Description graphql.String
			Color       graphql.String
		}
	} `graphql:"... on ProjectV2SingleSelectField"`

	ProjectV2IterationField struct {
		ID            graphql.String
		Name          graphql.String
		Configuration struct {
			Iterations []struct {
				ID        graphql.String
				Title     graphql.String
				StartDate graphql.String
				Duration  graphql.Int
			}
		}
	} `graphql:"... on ProjectV2IterationField"`
}

func convertFieldNode(node struct {
	Typename graphql.String `graphql:"__typename"`

	ProjectV2Field struct {
		ID       graphql.String
		Name     graphql.String
		DataType graphql.String
	} `graphql:"... on ProjectV2Field"`

	ProjectV2SingleSelectField struct {
		ID      graphql.String
		Name    graphql.String
		Options []struct {
			ID          graphql.String
			Name        graphql.String
			Description graphql.String
			Color       graphql.String
		}
	} `graphql:"... on ProjectV2SingleSelectField"`

	ProjectV2IterationField struct {
		ID            graphql.String
		Name          graphql.String
		Configuration struct {
			Iterations []struct {
				ID        graphql.String
				Title     graphql.String
				StartDate graphql.String
				Duration  graphql.Int
			}
		}
	} `graphql:"... on ProjectV2IterationField"`
}) *models.FieldSchema {
	typename := string(node.Typename)

	switch typename {
	case "ProjectV2Field":
		f := node.ProjectV2Field
		return &models.FieldSchema{
			ID:       string(f.ID),
			Name:     string(f.Name),
			DataType: models.FieldDataType(f.DataType),
		}

	case "ProjectV2SingleSelectField":
		f := node.ProjectV2SingleSelectField
		field := &models.FieldSchema{
			ID:       string(f.ID),
			Name:     string(f.Name),
			DataType: models.FieldTypeSingleSelect,
			Options:  make([]models.SelectOption, len(f.Options)),
		}
		for i, opt := range f.Options {
			field.Options[i] = models.SelectOption{
				ID:          string(opt.ID),
				Name:        string(opt.Name),
				Description: string(opt.Description),
				Color:       string(opt.Color),
			}
		}
		return field

	case "ProjectV2IterationField":
		f := node.ProjectV2IterationField
		field := &models.FieldSchema{
			ID:         string(f.ID),
			Name:       string(f.Name),
			DataType:   models.FieldTypeIteration,
			Iterations: make([]models.IterationInfo, len(f.Configuration.Iterations)),
		}
		for i, iter := range f.Configuration.Iterations {
			field.Iterations[i] = models.IterationInfo{
				ID:        string(iter.ID),
				Title:     string(iter.Title),
				StartDate: string(iter.StartDate),
				Duration:  int(iter.Duration),
			}
		}
		return field

	default:
		// Unknown field type, skip
		return nil
	}
}
