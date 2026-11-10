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
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthorOf(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		user *github.User
		want author
	}{
		"nil": {nil, author{}},
		"user": {
			&github.User{
				Login:   github.Ptr("mjudeikis"),
				HTMLURL: github.Ptr("https://github.com/mjudeikis"),
				Type:    github.Ptr("User"),
			},
			author{Login: "mjudeikis", URL: "https://github.com/mjudeikis"},
		},
		"bot": {
			&github.User{
				Login:   github.Ptr("renovate[bot]"),
				HTMLURL: github.Ptr("https://github.com/apps/renovate"),
				Type:    github.Ptr("Bot"),
			},
			author{Login: "renovate[bot]", URL: "https://github.com/apps/renovate", Bot: true},
		},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, cas.want, authorOf(cas.user))
		})
	}
}

func TestUpstreamCommitOf(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		commit *github.RepositoryCommit
		want   upstreamCommit
	}{
		"squash merge with author": {
			&github.RepositoryCommit{
				SHA: github.Ptr("db97262"),
				Commit: &github.Commit{
					Message: github.Ptr("chore(deps): update eslint monorepo to v10 (#362)\n\nCo-authored-by: x"),
				},
				Author: &github.User{
					Login:   github.Ptr("renovate[bot]"),
					HTMLURL: github.Ptr("https://github.com/apps/renovate"),
					Type:    github.Ptr("Bot"),
				},
			},
			upstreamCommit{
				SHA:    "db97262",
				Title:  "chore(deps): update eslint monorepo to v10",
				PR:     362,
				Author: author{Login: "renovate[bot]", URL: "https://github.com/apps/renovate", Bot: true},
			},
		},
		"merge commit": {
			&github.RepositoryCommit{
				SHA: github.Ptr("0c8c1a9"),
				Commit: &github.Commit{
					Message: github.Ptr("Merge pull request #137 from Perseus985/chore/updates-for-emudrha\n\nchore: updates for eMudhra"),
				},
				Author: &github.User{
					Login:   github.Ptr("Perseus985"),
					HTMLURL: github.Ptr("https://github.com/Perseus985"),
					Type:    github.Ptr("User"),
				},
			},
			upstreamCommit{
				SHA:    "0c8c1a9",
				Title:  "chore: updates for eMudhra",
				PR:     137,
				Author: author{Login: "Perseus985", URL: "https://github.com/Perseus985"},
			},
		},
		"no pull request and no GitHub author": {
			&github.RepositoryCommit{
				SHA: github.Ptr("490c369"),
				Commit: &github.Commit{
					Message: github.Ptr("fix(deps): update nestjs"),
					Author: &github.CommitAuthor{
						Name: github.Ptr("Jane Doe"),
					},
				},
			},
			upstreamCommit{
				SHA:    "490c369",
				Title:  "fix(deps): update nestjs",
				Author: author{Login: "Jane Doe"},
			},
		},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, cas.want, upstreamCommitOf(cas.commit))
		})
	}
}

func TestSplitPRSuffix(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		title     string
		wantTitle string
		wantPR    int
	}{
		"suffix":        {"Drop upstream images (#596)", "Drop upstream images", 596},
		"no suffix":     {"Target monorepo", "Target monorepo", 0},
		"number inside": {"Fix (#12) regression", "Fix (#12) regression", 0},
		"not a number":  {"Fix (#abc)", "Fix (#abc)", 0},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			gotTitle, gotPR := splitPRSuffix(cas.title)
			assert.Equal(t, cas.wantTitle, gotTitle)
			assert.Equal(t, cas.wantPR, gotPR)
		})
	}
}

// apiServer serves canned GitHub API responses and returns a client for it.
func apiServer(t *testing.T, handler http.Handler) *github.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := github.NewClient(nil)
	base, err := url.Parse(server.URL + "/")
	require.NoError(t, err)
	client.BaseURL = base
	return client
}

func TestPullRequest(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/platform-mesh/platform-mesh/pulls/594", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"number":594,"title":"Delete kcp-migration-operator","body":"notes","user":{"login":"mjudeikis","html_url":"https://github.com/mjudeikis","type":"User"}}`)
	})
	client := apiServer(t, mux)

	got, err := pullRequest(t.Context(), client, "platform-mesh", "platform-mesh", 594)
	require.NoError(t, err)
	want := pr{
		Number: 594,
		Title:  "Delete kcp-migration-operator",
		Body:   "notes",
		Author: author{Login: "mjudeikis", URL: "https://github.com/mjudeikis"},
	}
	assert.Equal(t, want, got)

	_, err = pullRequest(t.Context(), client, "platform-mesh", "platform-mesh", 1)
	require.Error(t, err, "unknown pull request")
}

func TestCompare(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/platform-mesh/portal/compare/v0.27.30...v0.27.32", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("Link", `<`+"http://"+r.Host+r.URL.Path+`?page=2>; rel="next"`)
			_, _ = fmt.Fprint(w, `{"commits":[{"sha":"db97262","commit":{"message":"chore(deps): update eslint monorepo to v10 (#362)"},"author":{"login":"renovate[bot]","html_url":"https://github.com/apps/renovate","type":"Bot"}}]}`)
		case "2":
			_, _ = fmt.Fprint(w, `{"commits":[{"sha":"490c369","commit":{"message":"fix(deps): update nestjs","author":{"name":"Jane Doe"}}}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	client := apiServer(t, mux)

	got, err := compare(t.Context(), client, "platform-mesh", "portal", "v0.27.30", "v0.27.32")
	require.NoError(t, err)
	want := []upstreamCommit{
		{
			SHA:    "db97262",
			Title:  "chore(deps): update eslint monorepo to v10",
			PR:     362,
			Author: author{Login: "renovate[bot]", URL: "https://github.com/apps/renovate", Bot: true},
		},
		{
			SHA:    "490c369",
			Title:  "fix(deps): update nestjs",
			Author: author{Login: "Jane Doe"},
		},
	}
	assert.Equal(t, want, got, "both pages are read")

	_, err = compare(t.Context(), client, "platform-mesh", "nope", "a", "b")
	require.Error(t, err, "unknown repository")
}

func TestNewGitHub(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	assert.NotNil(t, newGitHub(), "anonymous client")

	t.Setenv("GH_TOKEN", "token")
	assert.NotNil(t, newGitHub(), "authenticated client")
}

func TestOtherChanges(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/platform-mesh/portal/compare/v0.27.20...v0.27.32", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"commits":[{"sha":"db97262","commit":{"message":"fix: x (#1)"},"author":{"login":"jane","html_url":"https://github.com/jane","type":"User"}}]}`)
	})
	client := apiServer(t, mux)

	rows := []versionRow{
		{Name: "portal", From: "v0.27.20", To: "v0.27.32", Changed: true},
		{Name: "iam-ui", From: "v0.12.23", To: "v0.12.23"},
		{Name: "new-ui", From: absent, To: "v1.0.0", Changed: true},
		{Name: "kcp-access-vw", From: "0.1.0", To: "0.1.1", Changed: true},
	}
	got := otherChanges(t.Context(), client, "platform-mesh", rows)
	want := []upstreamChanges{
		{
			Chart: "portal",
			Repo:  "https://github.com/platform-mesh/portal",
			From:  "v0.27.20",
			To:    "v0.27.32",
			Commits: []upstreamCommit{
				{SHA: "db97262", Title: "fix: x", PR: 1, Author: author{Login: "jane", URL: "https://github.com/jane"}},
			},
		},
	}
	assert.Equal(t, want, got, "unchanged, newly added and unknown repositories are skipped")
	assert.Equal(t, []author{{Login: "jane", URL: "https://github.com/jane"}}, upstreamAuthors(got))
}
