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
)

// describePattern splits git describe --long output <tag>-<count>-g<sha>.
var describePattern = regexp.MustCompile(`^(v\S+?)-(\d+)-g([0-9a-f]+)$`)

// Previous returns the previous final release.
func Previous(ctx context.Context) (Version, error) {
	prev, _, _, err := describe(ctx, head)
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

// Now returns the version at the current commit.
// If the commit is tagged the tag is parsed and returned.
// Otherwise a prerelease version is returned.
func Now(ctx context.Context) (Version, error) {
	return FromCommit(ctx, head)
}

// FromCommit returns the version at a commit.
// If the commit is tagged the tag is parsed and returned.
// Otherwise a prerelease version is built from the nearest tagged ancestor.
//
// Warning: The base follows the branch of the checkout, not of the
// commit. This could produce erroneous results in an anomalous
// checkout.
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

	prev, count, sha, err := describe(ctx, commit)
	if err != nil {
		return Version{}, err
	}

	branch, err := branch(ctx)
	if err != nil {
		return Version{}, err
	}
	if s := os.Getenv(envRunNumber); s != "" {
		count, err = strconv.Atoi(s)
		if err != nil {
			return Version{}, fmt.Errorf("parsing %s: %w", envRunNumber, err)
		}
	}

	base := prereleaseBase(branch, prev)
	base.Pre = fmt.Sprintf("dev.%d.g%s", count, sha)
	return base, nil
}

// prereleaseBase returns the release currently being built towards.
// On a release branch it is the next patch release for that release branch.
// Otherwise the first release of the next feature release.
func prereleaseBase(branch string, prev Version) Version {
	base, onRelease := ParseReleaseBranch(branch)
	switch {
	case onRelease && prev.SameMinor(base):
		base.Patch = prev.Patch + 1
		return base
	case onRelease:
		return base
	default:
		return prev.NextMinor()
	}
}

// describe returns the previous final release of a commit, the number of
// commits since it and the shortened commit hash.
func describe(ctx context.Context, commit string) (Version, int, string, error) {
	out, err := releaseutils.Git(ctx, "describe", "--tags", "--long", "--abbrev=7", "--match", "v[0-9]*", "--exclude", "*-*", commit)
	if err != nil {
		return Version{}, 0, "", fmt.Errorf("finding previous release of %s: %w", commit, err)
	}
	return parseDescribe(strings.TrimSpace(out))
}

func parseDescribe(out string) (Version, int, string, error) {
	m := describePattern.FindStringSubmatch(out)
	if m == nil {
		return Version{}, 0, "", fmt.Errorf("unexpected git describe output %q", out)
	}
	prev, err := ParseTag(m[1])
	if err != nil {
		return Version{}, 0, "", fmt.Errorf("parsing previous release: %w", err)
	}
	count, _ := strconv.Atoi(m[2])
	return prev, count, m[3], nil
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
