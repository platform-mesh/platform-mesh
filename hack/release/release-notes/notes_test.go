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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReleaseNoteOf(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body   string
		want   []string
		wantOK bool
	}{
		"note":                 {"## Release Notes\n\n```release-note\nDelete kcp-migration-operator\n```\n", []string{"Delete kcp-migration-operator"}, true},
		"NONE":                 {"```release-note\nNONE\n```", nil, false},
		"none lowercase":       {"```release-note\n  none \n```", nil, false},
		"empty block":          {"```release-note\n\n```", nil, false},
		"missing block":        {"Automated appVersion bump.", nil, false},
		"multi-line with CRLF": {"```release-note\r\nBREAKING: a\r\n\r\nb\r\n```\r\n", []string{"BREAKING: a", "b"}, true},
		"second block ignored": {"```release-note\nfirst\n```\n```release-note\nsecond\n```", []string{"first"}, true},
		"unterminated block":   {"```release-note\nlast line", []string{"last line"}, true},
		"indented fences":      {"  ```release-note  \n  note\n  ```", []string{"note"}, true},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			got, ok := releaseNoteOf(cas.body)
			assert.Equal(t, cas.wantOK, ok)
			assert.Equal(t, cas.want, got)
		})
	}
}

func TestReleaseNotes(t *testing.T) {
	t.Parallel()

	changes := []change{
		{SHA: "a", Title: "newest", PR: 3},
		{SHA: "b", Title: "direct"},
		{SHA: "c", Title: "none", PR: 2},
		{SHA: "d", Title: "oldest", PR: 1},
		{SHA: "e", Title: "duplicate", PR: 3},
		{SHA: "f", Title: "unknown", PR: 9},
	}
	prs := map[int]pr{
		1: {Number: 1, Body: "```release-note\none\n```"},
		2: {Number: 2, Body: "```release-note\nNONE\n```"},
		3: {Number: 3, Body: "```release-note\nthree\nmore\n```"},
	}

	want := []releaseNote{
		{Lines: []string{"three", "more"}, PR: 3},
		{Lines: []string{"one"}, PR: 1},
	}
	assert.Equal(t, want, releaseNotes(changes, prs), "change order, NONE, direct, duplicate and unread pull requests skipped")
}

func TestContributors(t *testing.T) {
	t.Parallel()

	got := contributors(
		[]author{
			{Login: "ntnn", URL: "https://github.com/ntnn"},
			{Login: "renovate[bot]", URL: "https://github.com/apps/renovate", Bot: true},
			{Login: "Perseus985", URL: "https://github.com/Perseus985"},
		},
		[]author{
			{Login: "NTNN", URL: "https://github.com/NTNN"},
			{Login: "akafazov", URL: "https://github.com/akafazov"},
			{Login: ""},
		},
	)
	want := []author{
		{Login: "akafazov", URL: "https://github.com/akafazov"},
		{Login: "ntnn", URL: "https://github.com/ntnn"},
		{Login: "Perseus985", URL: "https://github.com/Perseus985"},
	}
	assert.Equal(t, want, got, "bots and empty logins dropped, case-insensitive dedupe keeps the first, case-insensitive sort")
}

func TestRepoOwnerName(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		url       string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		"default":    {"https://github.com/platform-mesh/platform-mesh", "platform-mesh", "platform-mesh", false},
		"git suffix": {"https://github.com/ntnn/platform-mesh.git", "ntnn", "platform-mesh", false},
		"no name":    {"https://github.com/platform-mesh", "", "", true},
		"too deep":   {"https://github.com/a/b/c", "", "", true},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			owner, name, err := repoOwnerName(cas.url)
			if cas.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, cas.wantOwner, owner)
			assert.Equal(t, cas.wantName, name)
		})
	}
}
