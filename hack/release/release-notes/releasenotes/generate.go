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
	"io"
	"strings"

	"github.com/google/go-github/v84/github"
)

// Options selects the range and the repository of the notes.
type Options struct {
	Start Ref
	End   Ref
	// RepoURL is the GitHub repository the commits belong to.
	RepoURL string
	Client  *github.Client
}

func (o *Options) validate() error {
	if o.Start.Commit == "" || o.End.Commit == "" {
		return errors.New("start and end refs are required")
	}
	if o.RepoURL == "" {
		return errors.New("repository URL is required")
	}
	if o.Client == nil {
		o.Client = NewGitHub()
	}
	o.RepoURL = strings.TrimSuffix(o.RepoURL, ".git")
	return nil
}

// Generate writes the release notes for opts to w.
// Incomplete notes are an error, never partial output.
func Generate(ctx context.Context, opts Options, w io.Writer) error {
	if err := opts.validate(); err != nil {
		return fmt.Errorf("invalid release notes options: %w", err)
	}
	start, end := opts.Start, opts.End
	if err := CheckRange(ctx, start, end); err != nil {
		return err
	}
	owner, repo, err := RepoOwnerName(opts.RepoURL)
	if err != nil {
		return err
	}

	changes, err := FirstParentLog(ctx, opts.RepoURL, start.Commit, end.Commit)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return fmt.Errorf("no commits between %s and %s", start.Version, end.Version)
	}
	comps, err := Components(ctx, end.Commit)
	if err != nil {
		return err
	}
	grouped, err := GroupChanges(ctx, comps, changes)
	if err != nil {
		return err
	}
	prs, err := collectPRs(ctx, opts.Client, owner, repo, changes)
	if err != nil {
		return err
	}

	otherComponents, err := OtherComponentRows(ctx, start.Commit, end.Commit)
	if err != nil {
		return err
	}
	other, err := otherChanges(ctx, opts.Client, otherComponents)
	if err != nil {
		return err
	}
	thirdPartyFrom, err := ThirdPartyVersions(ctx, start.Commit)
	if err != nil {
		return err
	}
	thirdPartyTo, err := ThirdPartyVersions(ctx, end.Commit)
	if err != nil {
		return err
	}

	n := notes{
		Version:         end.Version.String(),
		OCMVersion:      end.OCMVersion(),
		StartVersion:    start.Version.String(),
		StartOCMVersion: start.OCMVersion(),
		RepoURL:         opts.RepoURL,
		ReleaseNotes:    releaseNotes(changes, prs),
		Contributors:    contributors(prAuthors(prs), upstreamAuthors(other)),
		Components:      shippedComponents(comps),
		OtherComponents: otherComponents,
		ThirdParty:      thirdPartyRows(thirdPartyFrom, thirdPartyTo),
		Changes:         mergeUpstream(grouped, other),
	}
	return render(w, n)
}
