//go:build e2e

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

// Package deployer is the multi-cluster platform-mesh-deployer e2e suite. It
// creates its own kind clusters through hack/kind.sh (one config plane plus up
// to three workload clusters per test) and runs the deployer in-process, so it
// does not use the Tilt-deployed cluster the other tests/e2e suites target.
// It runs nightly and locally via `task tests:e2e:deployer`.
package deployer

import (
	"fmt"
	"os"
	"testing"
)

// TestMain skips the suite unless E2E_DEPLOYER is set (task tests:e2e:deployer
// sets it), so `go test -tags e2e ./e2e/...` against a Tilt cluster does not
// also bring up the deployer's kind clusters.
func TestMain(m *testing.M) {
	if os.Getenv("E2E_DEPLOYER") == "" {
		fmt.Println("skipping the deployer e2e suite: set E2E_DEPLOYER=true or run `task tests:e2e:deployer`")
		os.Exit(0)
	}
	os.Exit(m.Run())
}
