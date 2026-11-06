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

package suite

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// moduleAppDockerfile builds a scratch image around the statically linked
// binary; the binary itself is compiled on the host so the docker context stays
// a single file.
const moduleAppDockerfile = `FROM scratch
COPY module-app /module-app
ENTRYPOINT ["/module-app"]
`

// InstallRegistry deploys an OCI registry on the config plane and waits for it.
// OCMModule component versions are published there and resolved by the deployer,
// which runs on the host and so reaches it through a port-forward.
func (e *Env) InstallRegistry(t *testing.T) {
	t.Helper()
	applyKustomizeNS(t, e.Config, base("bases", "registry"), ProviderNamespace)
	rolloutWait(t, e.Config, ProviderNamespace, "deployment/registry")

	addr, err := forward(t, e.Config, registry)
	require.NoError(t, err)
	e.registryAddr = addr

	url := e.RegistryURL() + "/v2/"
	require.Eventuallyf(t, func() bool {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if err != nil {
			return false
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close() //nolint:errcheck // probe
		return resp.StatusCode == http.StatusOK
	}, 2*time.Minute, 2*time.Second, "registry not reachable on %s", url)
}

// BuildModuleApp builds the e2e module workload and loads it into every
// cluster, returning the image reference to put into the OCMModule's values.
// The tag is the source hash, so an unchanged app is neither rebuilt nor
// reloaded.
func (e *Env) BuildModuleApp(t *testing.T) string {
	t.Helper()

	src := filepath.Join(testdataDir(), "module-app")
	image := "pm-e2e-module-app:" + sourceHash(t, src)

	if !imageExists(image) {
		dir := t.TempDir()
		build := exec.Command("go", "build", "-o", filepath.Join(dir, "module-app"), "./"+filepath.Base(src)) //nolint:gosec // test-controlled args
		build.Dir = filepath.Dir(src)
		build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("building module-app:\n%s", out)
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(moduleAppDockerfile), 0o600))
		sh(t, "docker", "build", "-t", image, dir)
	}

	for _, c := range e.clusters() {
		sh(t, kindBinary(), "load", "docker-image", "--name", c.Name, image)
	}
	return image
}

// clusters returns every cluster of the environment, the config plane included
// and without repeating it when it doubles as a workload cluster.
func (e *Env) clusters() []*Cluster {
	out := []*Cluster{e.Config}
	for _, w := range e.Workloads {
		if w != e.Config {
			out = append(out, w)
		}
	}
	return out
}

func imageExists(image string) bool {
	return exec.Command("docker", "image", "inspect", image).Run() == nil //nolint:gosec // test-controlled args
}

// sourceHash hashes every file in dir so the image tag changes with the source.
func sourceHash(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // test-controlled path
		require.NoError(t, err)
		// Hashing to a sha256 never fails.
		_, _ = fmt.Fprintf(h, "%s\n%s\n", entry.Name(), content)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
