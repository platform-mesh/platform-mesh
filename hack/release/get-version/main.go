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

// get-version prints a Platform Mesh version derived from git tags and
// the current branch as per RFC 015.
//
// Usage: get-version [-library] current|previous|next
//
//	current   the release tag at HEAD
//	          or next patch on a release-YY.MM branch
//	          otherwise vYY.MM.9999-dev.<n>.g<sha>
//	previous  the previous final release vYY.MM.P
//	next      the next feature release vYY.MM
//	-library  print the Go module form v0.YYMM.P[-pre] instead;
//	          next prints its first release v0.YYMM.0
//
// Run from the repository root.
//
// For github actions the environment variables `GITHUB_REF_NAME` and
// `GITHUB_RUN_NUMBER` are handled if they are set:
//
//	GITHUB_REF_NAME    branch name, when the checkout is detached
//	GITHUB_RUN_NUMBER  <n> of a prerelease; commits since the previous release unless set
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
	"syscall"

	"go.platform-mesh.io/platform-mesh/hack/release/get-version/pmversions"
)

func main() {
	log.SetFlags(0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil {
		log.SetPrefix("error: ")
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("get-version", flag.ContinueOnError)
	library := flags.Bool("library", false, "print the Go module form v0.YYMM.P[-pre]")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}
	if flags.NArg() != 1 {
		return errors.New("usage: get-version [-library] current|previous|next")
	}

	var v pmversions.Version
	var err error
	switch flags.Arg(0) {
	case "current":
		v, err = pmversions.Current(ctx)
	case "previous":
		v, err = pmversions.Previous(ctx)
	case "next":
		v, err = pmversions.Next(ctx)
	default:
		return fmt.Errorf("unknown argument %q, want current, previous or next", flags.Arg(0))
	}
	if err != nil {
		return err
	}

	out := v.String()
	switch {
	case *library:
		out = v.LibraryString()
	case flags.Arg(0) == "next":
		out = v.Minor()
	}
	if _, err := fmt.Fprintln(stdout, out); err != nil {
		return fmt.Errorf("writing version: %w", err)
	}
	return nil
}
