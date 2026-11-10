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
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

const (
	// componentsFile registers the components built from this repository.
	componentsFile = "ocm/components.yaml"
	// repositoryComponent collects changes outside any component.
	repositoryComponent = "repository"
	// thirdPartyComponent collects changes to third_party/.
	thirdPartyComponent = "third-party"
	// multipleComponents collects changes touching more components than maxComponents.
	multipleComponents = "multiple components"
	maxComponents      = 3
)

var (
	mergeSubject = regexp.MustCompile(`^Merge pull request #(\d+) `)
	// appVersionBump is the title of the automated chart bumps; the version
	// tables already show those versions.
	appVersionBump = regexp.MustCompile(`^(\[BACKPORT\] )?Set appVersion \S+ in charts/[^/]+/Chart\.yaml$`)
	// housekeeping are commit titles without user-facing content.
	housekeeping = regexp.MustCompile(`(?i)^(tidy|go mod tidy|codegen|generate|regenerate|lint|fmt|gofmt)$`)
)

// change is one commit between the two refs.
type change struct {
	SHA   string
	Title string
	// PR is 0 for a commit that was not merged through a pull request.
	PR int
	// Repo is the GitHub repository the commit and pull request link to.
	Repo string
}

// isNoise reports whether a commit title carries nothing worth listing.
func isNoise(title string) bool {
	return appVersionBump.MatchString(title) || housekeeping.MatchString(title)
}

// component is an entry of ocm/components.yaml.
type component struct {
	Name    string
	Path    string `yaml:"path"`
	Chart   string `yaml:"chart"`
	Image   string `yaml:"image"`
	Library bool   `yaml:"library"`
}

// componentChanges are the changes attributed to one component.
type componentChanges struct {
	Component string
	// From and To are the versions of a component built in another
	// repository; empty for components built here.
	From    string
	To      string
	Changes []change
}

// firstParentLog returns the first-parent commits in start..end, newest first,
// linking to repoURL.
func firstParentLog(ctx context.Context, repoURL, start, end string) ([]change, error) {
	out, err := releaseutils.Git(ctx, "log", "--first-parent", "--format=%H%x1f%s%x1f%b%x1e", start+".."+end)
	if err != nil {
		return nil, fmt.Errorf("listing commits %s..%s: %w", start, end, err)
	}
	var changes []change
	for entry := range strings.SplitSeq(out, "\x1e") {
		fields := strings.SplitN(strings.TrimLeft(entry, "\n"), "\x1f", 3)
		if len(fields) != 3 {
			continue
		}
		title, number := parseLogEntry(fields[1], fields[2])
		changes = append(changes, change{
			SHA:   fields[0],
			Title: title,
			PR:    number,
			Repo:  repoURL,
		})
	}
	return changes, nil
}

// parseLogEntry returns the pull request title and number of a commit:
// a merge commit carries the number in its subject and the title as the
// first body line, a squash commit carries both in the subject.
func parseLogEntry(subject, body string) (string, int) {
	if m := mergeSubject.FindStringSubmatch(subject); m != nil {
		title, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
		if title = strings.TrimSpace(title); title == "" {
			title = subject
		}
		number, _ := strconv.Atoi(m[1])
		return title, number
	}
	return splitPRSuffix(subject)
}

// components reads ocm/components.yaml at a commit.
func components(ctx context.Context, commit string) ([]component, error) {
	out, err := releaseutils.Git(ctx, "show", commit+":"+componentsFile)
	if err != nil {
		return nil, fmt.Errorf("reading %s at %s: %w", componentsFile, commit, err)
	}
	var file struct {
		Components map[string]component `yaml:"components"`
	}
	if err := yaml.Unmarshal([]byte(out), &file); err != nil {
		return nil, fmt.Errorf("parsing %s at %s: %w", componentsFile, commit, err)
	}
	comps := make([]component, 0, len(file.Components))
	for name, c := range file.Components {
		c.Name = name
		comps = append(comps, c)
	}
	slices.SortFunc(comps, func(a, b component) int { return strings.Compare(a.Name, b.Name) })
	return comps, nil
}

// componentOf names the component a changed file belongs to.
func componentOf(path string, comps []component) string {
	best, bestLen := "", 0
	for _, c := range comps {
		for _, dir := range []string{c.Path, c.Chart} {
			if dir == "" || dir == "." || len(dir) <= bestLen {
				continue
			}
			if strings.HasPrefix(path, dir+"/") {
				best, bestLen = c.Name, len(dir)
			}
		}
	}
	if best != "" {
		return best
	}
	switch {
	case strings.HasPrefix(path, "charts/") && strings.Count(path, "/") > 1:
		return strings.SplitN(path, "/", 3)[1]
	case strings.HasPrefix(path, "third_party/"):
		return thirdPartyComponent
	default:
		return repositoryComponent
	}
}

// changedPaths lists the files a first-parent commit changed.
func changedPaths(ctx context.Context, sha string) ([]string, error) {
	out, err := releaseutils.Git(ctx, "diff-tree", "--no-commit-id", "--name-only", "-r", "-m", "--first-parent", sha)
	if err != nil {
		return nil, fmt.Errorf("listing files of %s: %w", sha, err)
	}
	return strings.Fields(out), nil
}

// groupChanges attributes every change to the components it touched,
// sorted by component name. Noise is dropped.
func groupChanges(ctx context.Context, comps []component, changes []change) ([]componentChanges, error) {
	byComponent := map[string][]change{}
	for _, ch := range changes {
		if isNoise(ch.Title) {
			continue
		}
		paths, err := changedPaths(ctx, ch.SHA)
		if err != nil {
			return nil, err
		}
		names := map[string]bool{}
		for _, p := range paths {
			names[componentOf(p, comps)] = true
		}
		switch {
		case len(names) == 0:
			byComponent[repositoryComponent] = append(byComponent[repositoryComponent], ch)
		case len(names) > maxComponents:
			byComponent[multipleComponents] = append(byComponent[multipleComponents], ch)
		default:
			for name := range names {
				byComponent[name] = append(byComponent[name], ch)
			}
		}
	}
	grouped := make([]componentChanges, 0, len(byComponent))
	for name, chs := range byComponent {
		grouped = append(grouped, componentChanges{
			Component: name,
			Changes:   chs,
		})
	}
	sortGroups(grouped)
	return grouped, nil
}

// mergeUpstream adds the commits of the components built in other
// repositories to their groups, ahead of the changes made here.
func mergeUpstream(grouped []componentChanges, other []upstreamChanges) []componentChanges {
	for _, uc := range other {
		var changes []change
		for _, c := range uc.Commits {
			if isNoise(c.Title) {
				continue
			}
			changes = append(changes, change{
				SHA:   c.SHA,
				Title: c.Title,
				PR:    c.PR,
				Repo:  uc.Repo,
			})
		}
		i := slices.IndexFunc(grouped, func(g componentChanges) bool { return g.Component == uc.Chart })
		if i < 0 {
			grouped = append(grouped, componentChanges{Component: uc.Chart})
			i = len(grouped) - 1
		}
		grouped[i].From = uc.From
		grouped[i].To = uc.To
		grouped[i].Changes = append(changes, grouped[i].Changes...)
	}
	sortGroups(grouped)
	return grouped
}

func sortGroups(grouped []componentChanges) {
	slices.SortFunc(grouped, func(a, b componentChanges) int { return strings.Compare(a.Component, b.Component) })
}
