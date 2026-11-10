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
	"log"
	"slices"
	"strings"

	"github.com/google/go-github/v84/github"
)

const (
	// releaseNoteFence opens the release note block of a pull request description.
	releaseNoteFence = "```release-note"
	// noReleaseNote marks a pull request without a user-facing change.
	noReleaseNote = "NONE"
)

// releaseNote is the release-note block of one pull request.
type releaseNote struct {
	Lines []string
	PR    int
}

// releaseNoteOf extracts the first release-note block of a pull request
// description, without blank lines.
// Returns false when there is no block or it says NONE.
func releaseNoteOf(body string) ([]string, bool) {
	var lines []string
	inside := false
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r", ""), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !inside && trimmed == releaseNoteFence:
			inside = true
		case inside && trimmed == "```":
			return finishReleaseNote(lines)
		case inside && trimmed != "":
			lines = append(lines, trimmed)
		}
	}
	if !inside {
		return nil, false
	}
	return finishReleaseNote(lines)
}

func finishReleaseNote(lines []string) ([]string, bool) {
	if len(lines) == 0 || (len(lines) == 1 && strings.EqualFold(lines[0], noReleaseNote)) {
		return nil, false
	}
	return lines, true
}

// collectPRs reads every pull request the changes were merged through.
// Failures are logged and leave the pull request out.
func collectPRs(ctx context.Context, client *github.Client, owner, repo string, changes []change) map[int]pr {
	prs := map[int]pr{}
	for _, ch := range changes {
		if ch.PR == 0 {
			continue
		}
		if _, ok := prs[ch.PR]; ok {
			continue
		}
		p, err := pullRequest(ctx, client, owner, repo, ch.PR)
		if err != nil {
			log.Printf("warning: %v", err)
			continue
		}
		prs[ch.PR] = p
	}
	return prs
}

// releaseNotes returns the release notes of the changes, in change order.
func releaseNotes(changes []change, prs map[int]pr) []releaseNote {
	var result []releaseNote
	seen := map[int]bool{}
	for _, ch := range changes {
		p, ok := prs[ch.PR]
		if !ok || seen[ch.PR] {
			continue
		}
		seen[ch.PR] = true
		lines, ok := releaseNoteOf(p.Body)
		if !ok {
			continue
		}
		result = append(result, releaseNote{
			Lines: lines,
			PR:    p.Number,
		})
	}
	return result
}

// contributors merges authors, drops bots and duplicates and sorts by login.
func contributors(groups ...[]author) []author {
	byLogin := map[string]author{}
	for _, group := range groups {
		for _, a := range group {
			if a.Bot || a.Login == "" {
				continue
			}
			key := strings.ToLower(a.Login)
			if _, ok := byLogin[key]; !ok {
				byLogin[key] = a
			}
		}
	}
	result := make([]author, 0, len(byLogin))
	for _, a := range byLogin {
		result = append(result, a)
	}
	slices.SortFunc(result, func(a, b author) int {
		return strings.Compare(strings.ToLower(a.Login), strings.ToLower(b.Login))
	})
	return result
}

// prAuthors lists the authors of the pull requests.
func prAuthors(prs map[int]pr) []author {
	authors := make([]author, 0, len(prs))
	for _, p := range prs {
		authors = append(authors, p.Author)
	}
	return authors
}
