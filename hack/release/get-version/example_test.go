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
	"os"

	"go.platform-mesh.io/platform-mesh/hack/release/get-version/scratchrepo"
)

// scratchRepo creates a fresh git repository with two commits in
// a temporary dir and cd's into it.
//
// One tagged v26.12.0 and one v26.12.1-rc.1.
//
// The returned function restores the previous working directory and
// deletes the tempdir.
func scratchRepo() func() {
	ctx := context.Background()
	repo, err := scratchrepo.New(ctx)
	if err != nil {
		panic(err)
	}
	for _, step := range []func() error{
		func() error { return repo.Commit(ctx, "release") },
		func() error { return repo.Tag(ctx, "v26.12.0") },
		func() error { return repo.Commit(ctx, "fix") },
		func() error { return repo.Tag(ctx, "v26.12.1-rc.1") },
	} {
		if err := step(); err != nil {
			panic(err)
		}
	}
	return func() {
		if err := repo.Close(); err != nil {
			panic(err)
		}
	}
}

func Example_run_now() {
	restore := scratchRepo()
	defer restore()

	if err := run(context.Background(), []string{"now"}, os.Stdout); err != nil {
		fmt.Println("error:", err)
	}
	// Output: v26.12.1-rc.1
}

func Example_run_previous() {
	restore := scratchRepo()
	defer restore()

	if err := run(context.Background(), []string{"previous"}, os.Stdout); err != nil {
		fmt.Println("error:", err)
	}
	// Output: v26.12.0
}

func Example_run_next() {
	restore := scratchRepo()
	defer restore()

	if err := run(context.Background(), []string{"next"}, os.Stdout); err != nil {
		fmt.Println("error:", err)
	}
	// Output: v27.01
}

func Example_run_library() {
	restore := scratchRepo()
	defer restore()

	for _, command := range []string{"now", "previous", "next"} {
		if err := run(context.Background(), []string{"-library", command}, os.Stdout); err != nil {
			fmt.Println("error:", err)
		}
	}
	// Output:
	// v0.2612.1-rc.1
	// v0.2612.0
	// v0.2701.0
}

func Example_run_usage() {
	if err := run(context.Background(), nil, os.Stdout); err != nil {
		fmt.Println("error:", err)
	}
	if err := run(context.Background(), []string{"latest"}, os.Stdout); err != nil {
		fmt.Println("error:", err)
	}
	// Output:
	// error: usage: get-version [-library] now|previous|next
	// error: unknown argument "latest", want now, previous or next
}
