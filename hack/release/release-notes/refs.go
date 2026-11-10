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
	"errors"
	"fmt"
	"strings"

	"go.platform-mesh.io/platform-mesh/hack/release/get-version/pmversions"
	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

// ref is a resolved --start or --end argument.
type ref struct {
	commit  string
	version pmversions.Version
}

// ocmVersion is the version without the leading v, as OCM and charts use it.
func (r ref) ocmVersion() string {
	return strings.TrimPrefix(r.version.String(), "v")
}

// resolveRef resolves a commit or version tag to a commit and its version.
func resolveRef(ctx context.Context, input string) (ref, error) {
	if input == "" {
		return ref{}, errors.New("empty ref")
	}
	out, err := releaseutils.Git(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", input+"^{commit}")
	if err != nil {
		return ref{}, fmt.Errorf("unknown ref %q", input)
	}
	commit := strings.TrimSpace(out)
	version, err := pmversions.FromCommit(ctx, commit)
	if err != nil {
		return ref{}, fmt.Errorf("resolving version of %s: %w", input, err)
	}
	r := ref{
		commit:  commit,
		version: version,
	}
	return r, nil
}

// defaultStart returns the nearest final release before end,
// skipping the tags at end itself.
func defaultStart(ctx context.Context, end ref) (ref, error) {
	args := []string{"describe", "--tags", "--abbrev=0", "--match", "v[0-9]*", "--exclude", "*-*"}
	out, err := releaseutils.Git(ctx, "tag", "--points-at", end.commit)
	if err != nil {
		return ref{}, fmt.Errorf("listing tags at %s: %w", end.version, err)
	}
	for tag := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if tag != "" {
			args = append(args, "--exclude", tag)
		}
	}
	out, err = releaseutils.Git(ctx, append(args, end.commit)...)
	if err != nil {
		return ref{}, fmt.Errorf("finding the release before %s: %w", end.version, err)
	}
	return resolveRef(ctx, strings.TrimSpace(out))
}

// checkRange rejects a start that is not a strict ancestor of end.
func checkRange(ctx context.Context, start, end ref) error {
	if start.commit == end.commit {
		return fmt.Errorf("start and end are the same commit %s", start.version)
	}
	if _, err := releaseutils.Git(ctx, "merge-base", "--is-ancestor", start.commit, end.commit); err != nil {
		return fmt.Errorf("start %s is not an ancestor of end %s", start.version, end.version)
	}
	return nil
}
