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

// release-notes prints the Markdown release notes for the commits
// between two refs.
//
// Usage: release-notes [--start <ref>] [--end <ref>]
//
//	--start  commit or version tag (vYY.MM.P) the notes start after;
//	         the nearest final release before --end unless set
//	--end    commit or version tag the notes end at; HEAD unless set
//
// Run from the repository root with the full history and tags fetched.
//
// Environment:
//
//	REPO_URL      GitHub repository the pull requests belong to, https://github.com/platform-mesh/platform-mesh unless set
//	GH_TOKEN      GitHub token for the API; GITHUB_TOKEN is used when unset. Optional, raises the rate limit
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix(">>> ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil {
		log.SetPrefix("error: ")
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("release-notes", flag.ContinueOnError)
	startFlag := flags.String("start", "", "commit or version tag the notes start after")
	endFlag := flags.String("end", "HEAD", "commit or version tag the notes end at")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}
	if flags.NArg() != 0 {
		return errors.New("usage: release-notes [--start <ref>] [--end <ref>]")
	}

	end, err := resolveRef(ctx, *endFlag)
	if err != nil {
		return err
	}

	var start ref
	if *startFlag == "" {
		start, err = defaultStart(ctx, end)
	} else {
		start, err = resolveRef(ctx, *startFlag)
	}
	if err != nil {
		return err
	}

	repoURL := strings.TrimSuffix(releaseutils.EnvOr("REPO_URL", releaseutils.DefaultRepoURL), ".git")
	owner, repo, err := repoOwnerName(repoURL)
	if err != nil {
		return err
	}
	client := newGitHub()

	changes, err := firstParentLog(ctx, start.commit, end.commit)
	if err != nil {
		return err
	}
	comps, err := components(ctx, end.commit)
	if err != nil {
		return err
	}
	grouped, err := groupChanges(ctx, comps, changes)
	if err != nil {
		return err
	}
	prs := collectPRs(ctx, client, owner, repo, changes)

	n := notes{
		Version:      end.version.String(),
		OCMVersion:   end.ocmVersion(),
		RepoURL:      repoURL,
		ReleaseNotes: releaseNotes(changes, prs),
		Contributors: contributors(prAuthors(prs)),
		Changes:      grouped,
	}
	return render(stdout, n)
}
