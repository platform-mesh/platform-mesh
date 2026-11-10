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
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/go-github/v84/github"
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

// repoOwnerName splits a GitHub repository URL into owner and name.
func repoOwnerName(repoURL string) (string, string, error) {
	path := strings.TrimPrefix(strings.TrimSuffix(repoURL, ".git"), "https://github.com/")
	owner, name, ok := strings.Cut(path, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("%q is not a GitHub repository URL", repoURL)
	}
	return owner, name, nil
}

// newGitHub returns a client authenticated with GH_TOKEN or GITHUB_TOKEN
// when either is set, anonymous otherwise.
func newGitHub() *github.Client {
	client := github.NewClient(nil)
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if token := os.Getenv(key); token != "" {
			return client.WithAuthToken(token)
		}
	}
	return client
}

func pullRequest(ctx context.Context, client *github.Client, owner, repo string, number int) (pr, error) {
	p, _, err := client.PullRequests.Get(ctx, owner, repo, number)
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
		comparison, resp, err := client.Repositories.CompareCommits(ctx, owner, repo, base, head, opts)
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
	title, _, _ := strings.Cut(c.GetCommit().GetMessage(), "\n")
	title, number := splitPRSuffix(strings.TrimSpace(title))
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
