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

package exporter

import (
	"encoding/csv"
	"io"
	"strconv"
	"strings"
)

// WriteCSV writes the export result as CSV to the given writer.
// Columns: number, type, title, state, url, closed_at, repository, labels
func WriteCSV(w io.Writer, result *ExportResult) error {
	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write header
	header := []string{"number", "type", "title", "state", "url", "closed_at", "repository", "labels"}
	if err := writer.Write(header); err != nil {
		return err
	}

	// Write rows
	for _, item := range result.Items {
		closedAt := ""
		if item.ClosedAt != nil {
			closedAt = item.ClosedAt.Format("2006-01-02")
		}

		repository := item.Repository.FullName()
		labels := strings.Join(item.Labels, ", ")

		row := []string{
			strconv.Itoa(item.Number),
			string(item.Type),
			item.Title,
			item.State,
			item.URL,
			closedAt,
			repository,
			labels,
		}

		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}
