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

package releaseutils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeOCM replaces the ocm CLI with a shell script and returns the file the
// script appends the arguments of each call to, one call per line.
// The script finds that file as $CALLS.
func fakeOCM(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	bin := filepath.Join(dir, "ocm")
	content := "#!/bin/sh\nCALLS=" + calls + "\necho \"$*\" >> \"$CALLS\"\n" + script
	if err := os.WriteFile(bin, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	oldBinary, oldDelay := ocmBinary, ocmRetryDelay
	ocmBinary, ocmRetryDelay = bin, 0
	t.Cleanup(func() { ocmBinary, ocmRetryDelay = oldBinary, oldDelay })
	return calls
}

func readCalls(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

const descriptors = `[{"component":{"name":"example.com/a","version":"1.0.0"}}]`

func TestAddRetries(t *testing.T) {
	calls := fakeOCM(t, `
if [ "$(wc -l < "$CALLS")" -eq 1 ]; then
  echo "Error: failed to tag manifest: sha256:abc: not found" >&2
  exit 1
fi
echo '`+descriptors+`'
`)
	cvs, err := OCMRepository{Repo: "example.com/repo"}.Add(t.Context(), "constructor.yaml", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cvs) != 1 || cvs[0] != "example.com/a:1.0.0" {
		t.Errorf("component versions = %v", cvs)
	}
	if got := readCalls(t, calls); len(got) != 2 {
		t.Errorf("ocm ran %d times, want 2: %v", len(got), got)
	}
}

func TestAddGivesUp(t *testing.T) {
	calls := fakeOCM(t, `echo "Error: failed to tag manifest: sha256:abc: not found" >&2; exit 1`)
	_, err := OCMRepository{Repo: "example.com/repo"}.Add(t.Context(), "constructor.yaml", "", nil)
	if err == nil {
		t.Fatal("no error from a build that keeps failing")
	}
	if got := readCalls(t, calls); len(got) != RegistryAttempts {
		t.Errorf("ocm ran %d times, want %d: %v", len(got), RegistryAttempts, got)
	}
}

func TestExists(t *testing.T) {
	tests := map[string]struct {
		script    string
		want      bool
		wantErr   bool
		wantCalls int
	}{
		"present": {script: "echo '{}'", want: true, wantCalls: 1},
		"absent": {
			script:    "echo 'Error: component version not found' >&2; exit 1",
			wantCalls: 1,
		},
		"registry failure is not absent": {
			script:    "echo 'Error: 503 service unavailable' >&2; exit 1",
			wantErr:   true,
			wantCalls: RegistryAttempts,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			calls := fakeOCM(t, tt.script)
			got, err := OCMRepository{Repo: "example.com/repo"}.Exists(t.Context(), "example.com/a:1.0.0")
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("exists = %v, want %v", got, tt.want)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "503 service unavailable") {
				t.Errorf("error does not carry ocm's stderr: %v", err)
			}
			if n := len(readCalls(t, calls)); n != tt.wantCalls {
				t.Errorf("ocm ran %d times, want %d", n, tt.wantCalls)
			}
		})
	}
}

func TestSignSkipsSigned(t *testing.T) {
	calls := fakeOCM(t, `
case "$1 $5" in
  "verify example.com/repo//example.com/signed:1.0.0") exit 0 ;;
  verify*) exit 1 ;;
esac
`)
	repo := OCMRepository{Repo: "example.com/repo", Signature: "sig"}
	if err := repo.Sign(t.Context(), []string{"example.com/signed:1.0.0", "example.com/unsigned:1.0.0"}); err != nil {
		t.Fatal(err)
	}
	got := readCalls(t, calls)
	want := []string{
		"verify component-version --signature sig example.com/repo//example.com/signed:1.0.0",
		"verify component-version --signature sig example.com/repo//example.com/unsigned:1.0.0",
		"sign component-version --signature sig example.com/repo//example.com/unsigned:1.0.0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("ocm calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
