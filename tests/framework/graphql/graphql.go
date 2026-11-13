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
// Package graphql sends a query to a GraphQL service of a deployed Platform
// Mesh, as a user. The packages iam and gateway are built on it.
package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.platform-mesh.io/platform-mesh/tests/framework/pm"
)

// Query sends the query to the GraphQL service of the organization org that
// is served at path (see pm.OrganizationURL), as the user with the ID
// token, and decodes the data of the answer into out. An answer with
// errors is an error.
func Query(ctx context.Context, inst pm.Installation, org, path, token, query string, out any) error {
	portal, client, err := inst.Portal(ctx)
	if err != nil {
		return err
	}
	target, err := pm.OrganizationURL(portal, org, path)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s: %s: %s", target, res.Status, strings.TrimSpace(string(data)))
	}
	var answer struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return fmt.Errorf("POST %s: decoding the answer: %w", target, err)
	}
	if len(answer.Errors) != 0 {
		messages := make([]string, 0, len(answer.Errors))
		for _, e := range answer.Errors {
			messages = append(messages, e.Message)
		}
		return fmt.Errorf("POST %s: %s", target, strings.Join(messages, "; "))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(answer.Data, out)
}
