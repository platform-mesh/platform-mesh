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

package pmversions

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

const (
	// envRefName is the branch or tag name GitHub Actions checked out,
	// the checkout itself is detached.
	envRefName = "GITHUB_REF_NAME"
	// envRunNumber numbers prerelease builds in GitHub Actions.
	envRunNumber = "GITHUB_RUN_NUMBER"
	// head is the commit the checkout is at.
	head = "HEAD"
	// DevelopmentPatch marks a build of a non-release branch, it sorts
	// above every patch release of its month.
	DevelopmentPatch = 9999
	// shaLength abbreviates commits in prerelease versions.
	shaLength = 7
)

// describePattern splits git describe --long output <tag>-<count>-g<sha>.
var describePattern = regexp.MustCompile(`^(v\S+?)-(\d+)-g([0-9a-f]+)$`)

// Previous returns the previous final release.
func Previous(ctx context.Context) (Version, error) {
	prev, _, err := describe(ctx, head)
	return prev, err
}

// Next returns the next possible release.
// If the previous release is v26.12<.patch> the result is v27.01.
func Next(ctx context.Context) (Version, error) {
	prev, err := Previous(ctx)
	if err != nil {
		return Version{}, err
	}
	return prev.NextMinor(), nil
}

// Current returns the version at the current commit.
// If the commit is tagged the tag is parsed and returned.
// Otherwise a prerelease version is returned.
func Current(ctx context.Context) (Version, error) {
	return FromCommit(ctx, head)
}

// FromCommit returns the version at a commit.
// If the commit is tagged the tag is parsed and returned.
//
// On a release branch an untagged commit gets the next patch of that
// release as <patch>-dev.<n>.g<sha>, counted from the nearest tagged
// ancestor.
//
// Anywhere else it gets vYY.MM.9999-dev.<n>.g<sha> from the commit
// date, so a checkout without tags still yields a usable version. <n>
// is GITHUB_RUN_NUMBER when set, else the commit count.
//
// Warning: the branch is that of the checkout, not of the commit.
func FromCommit(ctx context.Context, commit string) (Version, error) {
	out, err := releaseutils.Git(ctx, "tag", "-l", "v[0-9]*", "--points-at", commit)
	if err != nil {
		return Version{}, fmt.Errorf("listing release tags at %s: %w", commit, err)
	}

	var at []Version
	for line := range strings.SplitSeq(out, "\n") {
		if v, err := ParseTag(strings.TrimSpace(line)); err == nil {
			at = append(at, v)
		}
	}
	if len(at) > 0 {
		return slices.MaxFunc(at, Version.Compare), nil
	}

	branch, err := branch(ctx)
	if err != nil {
		return Version{}, err
	}
	var base Version
	var count int
	minor, onRelease := ParseReleaseBranch(branch)
	if onRelease {
		base, count, err = releaseBranchBase(ctx, commit, minor)
	} else {
		base, count, err = developmentBase(ctx, commit)
	}
	if err != nil {
		return Version{}, err
	}
	if s := os.Getenv(envRunNumber); s != "" {
		count, err = strconv.Atoi(s)
		if err != nil {
			return Version{}, fmt.Errorf("parsing %s: %w", envRunNumber, err)
		}
	}

	sha, err := releaseutils.Git(ctx, "rev-parse", "--short="+strconv.Itoa(shaLength), commit)
	if err != nil {
		return Version{}, fmt.Errorf("abbreviating %s: %w", commit, err)
	}

	base.Pre = fmt.Sprintf("dev.%d.g%s", count, strings.TrimSpace(sha))
	return base, nil
}

// releaseBranchBase returns the next patch of the release branch's minor
// and the commits since the nearest release.
func releaseBranchBase(ctx context.Context, commit string, minor Version) (Version, int, error) {
	prev, count, err := describe(ctx, commit)
	if err != nil {
		return Version{}, 0, err
	}
	if prev.SameMinor(minor) {
		minor.Patch = prev.Patch + 1
	}
	return minor, count, nil
}

// developmentBase returns vYY.MM.9999 for the month of the commit and the
// number of commits in its history.
func developmentBase(ctx context.Context, commit string) (Version, int, error) {
	out, err := releaseutils.Git(ctx, "show", "-s", "--format=%cs", commit)
	if err != nil {
		return Version{}, 0, fmt.Errorf("reading date of %s: %w", commit, err)
	}
	date, err := time.Parse(time.DateOnly, strings.TrimSpace(out))
	if err != nil {
		return Version{}, 0, fmt.Errorf("parsing date of %s: %w", commit, err)
	}
	out, err = releaseutils.Git(ctx, "rev-list", "--count", commit)
	if err != nil {
		return Version{}, 0, fmt.Errorf("counting commits of %s: %w", commit, err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return Version{}, 0, fmt.Errorf("parsing commit count of %s: %w", commit, err)
	}
	base := Version{
		Year:  date.Year() % 100,
		Month: int(date.Month()),
		Patch: DevelopmentPatch,
	}
	return base, count, nil
}

// describe returns the previous final release of a commit and the number
// of commits since it.
func describe(ctx context.Context, commit string) (Version, int, error) {
	out, err := releaseutils.Git(ctx, "describe", "--tags", "--long", "--abbrev=7", "--match", "v[0-9]*", "--exclude", "*-*", commit)
	if err != nil {
		return Version{}, 0, fmt.Errorf("finding previous release of %s: %w", commit, err)
	}
	return parseDescribe(strings.TrimSpace(out))
}

// parseDescribe splits <tag>-<count>-g<sha>.
func parseDescribe(out string) (Version, int, error) {
	m := describePattern.FindStringSubmatch(out)
	if m == nil {
		return Version{}, 0, fmt.Errorf("unexpected git describe output %q", out)
	}
	prev, err := ParseTag(m[1])
	if err != nil {
		return Version{}, 0, fmt.Errorf("parsing previous release: %w", err)
	}
	count, _ := strconv.Atoi(m[2])
	return prev, count, nil
}

func branch(ctx context.Context) (string, error) {
	if ref := os.Getenv(envRefName); ref != "" {
		return ref, nil
	}
	out, err := releaseutils.Git(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("reading current branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}
