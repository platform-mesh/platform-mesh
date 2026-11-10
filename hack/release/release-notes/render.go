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

package main

import (
	"fmt"
	"io"
	"strings"
	"text/template"

	_ "embed"
)

//go:embed notes.md.tmpl
var notesTemplate string

// notes is everything the release notes template renders.
type notes struct {
	// Version is the release, as vYY.MM.P[-pre].
	Version string
	// OCMVersion is the release without the leading v, as OCM and charts use it.
	OCMVersion string
	// StartVersion is the release the notes start after, as vYY.MM.P[-pre].
	StartVersion string
	// StartOCMVersion is StartVersion without the leading v.
	StartOCMVersion string
	// RepoURL is the GitHub repository pull requests and commits link to.
	RepoURL      string
	ReleaseNotes []releaseNote
	Contributors []author
	// Components are built from this repository, all at OCMVersion.
	Components      []string
	OtherComponents []versionRow
	ThirdParty      []versionRow
	Changes         []componentChanges
}

func render(w io.Writer, n notes) error {
	funcs := template.FuncMap{
		"join": strings.Join,
		"dict": dict,
	}
	tmpl, err := template.New("notes").Funcs(funcs).Parse(notesTemplate)
	if err != nil {
		return fmt.Errorf("parsing notes template: %w", err)
	}
	if err := tmpl.Execute(w, n); err != nil {
		return fmt.Errorf("rendering notes: %w", err)
	}
	return nil
}

// dict builds template arguments from alternating keys and values.
func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict needs key/value pairs, got %d arguments", len(pairs))
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict key %v is not a string", pairs[i])
		}
		m[key] = pairs[i+1]
	}
	return m, nil
}
