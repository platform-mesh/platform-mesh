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
// Usage: get-version now|previous|next
//
//	now       the release tag at HEAD, otherwise <base>-dev.<n>.g<sha>
//	previous  the previous final release vYY.MM.P
//	next      the next feature release vYY.MM
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
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.platform-mesh.io/platform-mesh/hack/release/get-version/pmversions"
)

func main() {
	log.SetFlags(0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		log.SetPrefix("error: ")
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	if len(os.Args) != 2 {
		return errors.New("usage: get-version now|previous|next")
	}

	var out string
	switch os.Args[1] {
	case "now":
		v, err := pmversions.Now(ctx)
		if err != nil {
			return err
		}
		out = v.String()
	case "previous":
		v, err := pmversions.Previous(ctx)
		if err != nil {
			return err
		}
		out = v.String()
	case "next":
		v, err := pmversions.Next(ctx)
		if err != nil {
			return err
		}
		out = v.Minor()
	default:
		return fmt.Errorf("unknown argument %q, want now, previous or next", os.Args[1])
	}
	fmt.Println(out)
	return nil
}
