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
	// RepoURL is the GitHub repository pull requests and commits link to.
	RepoURL      string
	ReleaseNotes []releaseNote
	Contributors []author
	Changes      []componentChanges
}

func render(w io.Writer, n notes) error {
	funcs := template.FuncMap{
		"join": strings.Join,
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
