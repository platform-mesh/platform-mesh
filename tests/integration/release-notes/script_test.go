//go:build integration

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
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestScript runs testdata/script/*.txtar against the release commands,
// built once from the repository and put on PATH.
func TestScript(t *testing.T) {
	bin := t.TempDir()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"get-version", "release-notes"} {
		build := exec.CommandContext(t.Context(), "go", "build", "-o", filepath.Join(bin, cmd), "go.platform-mesh.io/platform-mesh/hack/release/"+cmd)
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("building %s: %v: %s", cmd, err, out)
		}
	}

	testscript.Run(t, testscript.Params{
		Dir:                 "testdata/script",
		RequireExplicitExec: true,
		UpdateScripts:       os.Getenv("UPDATE") != "",
		Setup: func(env *testscript.Env) error {
			env.Setenv("PATH", bin+string(os.PathListSeparator)+env.Getenv("PATH"))
			// Keep the developer's git config, signing and hooks and the GitHub Actions variables out of the scripts.
			env.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			env.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			env.Setenv("GIT_AUTHOR_NAME", "script")
			env.Setenv("GIT_AUTHOR_EMAIL", "script@example.com")
			env.Setenv("GIT_COMMITTER_NAME", "script")
			env.Setenv("GIT_COMMITTER_EMAIL", "script@example.com")
			env.Setenv("GITHUB_REF_NAME", "")
			env.Setenv("GITHUB_RUN_NUMBER", "")
			return nil
		},
	})
}
