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

	"github.com/shurcooL/graphql"
	"golang.org/x/oauth2"
)

// Client wraps the GitHub GraphQL client
type Client struct {
	gql *graphql.Client
}

// NewClient creates a new GitHub GraphQL client with the provided token
func NewClient(token string) *Client {
	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	httpClient := oauth2.NewClient(context.Background(), src)

	return &Client{
		gql: graphql.NewClient("https://api.github.com/graphql", httpClient),
	}
}
