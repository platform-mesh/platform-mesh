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

package releasenotes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v84/github"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

// Retry policy for the GitHub API. Rate limits and client errors are not
// retried.
const (
	apiTimeout    = 30 * time.Second
	apiAttempts   = 3
	apiRetryDelay = 5 * time.Second
)

// prSuffix is the pull request number squash and merge commits carry.
var prSuffix = regexp.MustCompile(` \(#(\d+)\)$`)

// author is a GitHub user as listed under contributors.
type author struct {
	Login string
	URL   string
	Bot   bool
}

// pr is the part of a pull request the notes use.
type pr struct {
	Number int
	Title  string
	Body   string
	Author author
}

// upstreamCommit is a commit of a component built in another repository.
type upstreamCommit struct {
	SHA    string
	Title  string
	PR     int
	Author author
}

// RepoOwnerName splits a GitHub repository URL into owner and name.
func RepoOwnerName(repoURL string) (string, string, error) {
	path := strings.TrimPrefix(strings.TrimSuffix(repoURL, ".git"), "https://github.com/")
	owner, name, ok := strings.Cut(path, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("%q is not a GitHub repository URL", repoURL)
	}
	return owner, name, nil
}

// NewGitHub returns a client authenticated with GH_TOKEN or GITHUB_TOKEN
// when either is set, anonymous otherwise.
// apiTimeout bounds every request; go-github's default client has none,
// and a hung connection would otherwise stall the release job.
func NewGitHub() *github.Client {
	client := github.NewClient(&http.Client{Timeout: apiTimeout})
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if token := os.Getenv(key); token != "" {
			return client.WithAuthToken(token)
		}
	}
	return client
}

func pullRequest(ctx context.Context, client *github.Client, owner, repo string, number int) (pr, error) {
	var p *github.PullRequest
	err := callAPI(ctx, func() error {
		var err error
		p, _, err = client.PullRequests.Get(ctx, owner, repo, number)
		return err
	})
	if err != nil {
		return pr{}, fmt.Errorf("reading %s/%s#%d: %w", owner, repo, number, err)
	}
	result := pr{
		Number: number,
		Title:  p.GetTitle(),
		Body:   p.GetBody(),
		Author: authorOf(p.GetUser()),
	}
	return result, nil
}

// compare returns the commits in base...head.
func compare(ctx context.Context, client *github.Client, owner, repo, base, head string) ([]upstreamCommit, error) {
	var commits []upstreamCommit
	opts := &github.ListOptions{PerPage: 100}
	for {
		var comparison *github.CommitsComparison
		var resp *github.Response
		err := callAPI(ctx, func() error {
			var err error
			comparison, resp, err = client.Repositories.CompareCommits(ctx, owner, repo, base, head, opts)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("comparing %s/%s %s...%s: %w", owner, repo, base, head, err)
		}
		for _, c := range comparison.Commits {
			commits = append(commits, upstreamCommitOf(c))
		}
		if resp.NextPage == 0 {
			return commits, nil
		}
		opts.Page = resp.NextPage
	}
}

// callAPI runs one API call, retrying transient failures.
// Rate limits and client errors are returned at once.
func callAPI(ctx context.Context, fn func() error) error {
	return releaseutils.Retry(ctx, apiAttempts, apiRetryDelay, func() error {
		err := fn()
		switch {
		case err == nil:
			return nil
		case isRateLimited(err):
			return releaseutils.Permanent(fmt.Errorf("%w; set GH_TOKEN to raise the limit", err))
		case isClientError(err):
			return releaseutils.Permanent(err)
		default:
			return err
		}
	})
}

func isRateLimited(err error) bool {
	var rate *github.RateLimitError
	var abuse *github.AbuseRateLimitError
	return errors.As(err, &rate) || errors.As(err, &abuse)
}

func isClientError(err error) bool {
	var resp *github.ErrorResponse
	return errors.As(err, &resp) && resp.Response != nil &&
		resp.Response.StatusCode >= http.StatusBadRequest && resp.Response.StatusCode < http.StatusInternalServerError
}

// isNotFound reports whether the API answered 404.
func isNotFound(err error) bool {
	var resp *github.ErrorResponse
	return errors.As(err, &resp) && resp.Response != nil && resp.Response.StatusCode == http.StatusNotFound
}

func authorOf(user *github.User) author {
	if user == nil {
		return author{}
	}
	return author{
		Login: user.GetLogin(),
		URL:   user.GetHTMLURL(),
		Bot:   user.GetType() == "Bot",
	}
}

func upstreamCommitOf(c *github.RepositoryCommit) upstreamCommit {
	subject, body, _ := strings.Cut(c.GetCommit().GetMessage(), "\n")
	title, number := parseLogEntry(strings.TrimSpace(subject), body)
	commit := upstreamCommit{
		SHA:    c.GetSHA(),
		Title:  title,
		PR:     number,
		Author: authorOf(c.GetAuthor()),
	}
	if commit.Author.Login == "" {
		commit.Author.Login = c.GetCommit().GetAuthor().GetName()
	}
	return commit
}

// splitPRSuffix removes a trailing " (#N)" from a title and returns N,
// 0 when there is none.
func splitPRSuffix(title string) (string, int) {
	m := prSuffix.FindStringSubmatch(title)
	if m == nil {
		return title, 0
	}
	number, _ := strconv.Atoi(m[1])
	return strings.TrimSuffix(title, m[0]), number
}
