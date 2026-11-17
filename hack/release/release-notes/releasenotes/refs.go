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
	"strings"

	"go.platform-mesh.io/platform-mesh/hack/release/get-version/pmversions"
	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

// Ref is a resolved --start or --end argument.
type Ref struct {
	Commit  string
	Version pmversions.Version
}

// OCMVersion is the version without the leading v, as OCM and charts use it.
func (r Ref) OCMVersion() string {
	return strings.TrimPrefix(r.Version.String(), "v")
}

// ResolveRef resolves a commit or version tag to a commit and its version.
func ResolveRef(ctx context.Context, input string) (Ref, error) {
	if input == "" {
		return Ref{}, errors.New("empty ref")
	}
	out, err := releaseutils.Git(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", input+"^{commit}")
	if err != nil {
		return Ref{}, fmt.Errorf("unknown ref %q", input)
	}
	commit := strings.TrimSpace(out)
	version, err := pmversions.FromCommit(ctx, commit)
	if err != nil {
		return Ref{}, fmt.Errorf("resolving version of %s: %w", input, err)
	}
	r := Ref{
		Commit:  commit,
		Version: version,
	}
	return r, nil
}

// DefaultStart returns the nearest final release before end,
// skipping the tags at end itself.
func DefaultStart(ctx context.Context, end Ref) (Ref, error) {
	args := []string{"describe", "--tags", "--abbrev=0", "--match", "v[0-9]*", "--exclude", "*-*"}
	out, err := releaseutils.Git(ctx, "tag", "--points-at", end.Commit)
	if err != nil {
		return Ref{}, fmt.Errorf("listing tags at %s: %w", end.Version, err)
	}
	for tag := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if tag != "" {
			args = append(args, "--exclude", tag)
		}
	}
	out, err = releaseutils.Git(ctx, append(args, end.Commit)...)
	if err != nil {
		return Ref{}, fmt.Errorf("finding the release before %s: %w", end.Version, err)
	}
	return ResolveRef(ctx, strings.TrimSpace(out))
}

// CheckRange rejects a start that is not a strict ancestor of end.
func CheckRange(ctx context.Context, start, end Ref) error {
	if start.Commit == end.Commit {
		return fmt.Errorf("start and end are the same commit %s", start.Version)
	}
	if _, err := releaseutils.Git(ctx, "merge-base", "--is-ancestor", start.Commit, end.Commit); err != nil {
		return fmt.Errorf("start %s is not an ancestor of end %s", start.Version, end.Version)
	}
	return nil
}
