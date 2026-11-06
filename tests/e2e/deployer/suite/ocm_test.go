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

package suite

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ociurl "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/repository"

	pmocm "go.platform-mesh.io/platform-mesh-deployer/pkg/ocm"
)

func smokeComponent() Component {
	return Component{
		Name:      "github.com/platform-mesh/e2e-acme",
		Version:   "0.1.0",
		Resources: map[string]string{"app-manifests": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"},
	}
}

// newCTF creates an empty CTF in a temporary directory.
func newCTF(t *testing.T) repository.ComponentVersionRepository {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ctf")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	archive, err := ctf.OpenCTFFromOSPath(dir, os.O_RDWR|os.O_CREATE)
	require.NoError(t, err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	require.NoError(t, err)
	return repo
}

func readBlob(t *testing.T, cv interface{ ReadCloser() (io.ReadCloser, error) }) string {
	t.Helper()
	rc, err := cv.ReadCloser()
	require.NoError(t, err)
	defer rc.Close() //nolint:errcheck // read-only
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return string(data)
}

// TestBuildComponent covers packaging a component version into a CTF, which is
// what the e2e caches between runs.
func TestBuildComponent(t *testing.T) {
	c := smokeComponent()
	repo := newCTF(t)
	require.NoError(t, build(t.Context(), repo, c))

	desc, err := repo.GetComponentVersion(t.Context(), c.Name, c.Version)
	require.NoError(t, err)
	require.Len(t, desc.Component.Resources, 1)
	require.Equal(t, "app-manifests", desc.Component.Resources[0].Name)

	blob, _, err := repo.GetLocalResource(t.Context(), c.Name, c.Version, desc.Component.Resources[0].ToIdentity())
	require.NoError(t, err)
	require.Equal(t, c.Resources["app-manifests"], readBlob(t, blob))
}

// TestTransferComponent publishes a CTF into a registry and reads it back
// through the deployer's own resolver, which is the path the e2e depends on.
// It needs a registry, e.g. `docker run -d -p 5111:5000 registry:2`.
func TestTransferComponent(t *testing.T) {
	url := os.Getenv("SMOKE_REGISTRY")
	if url == "" {
		t.Skip("SMOKE_REGISTRY not set, e.g. http://localhost:5111")
	}

	c := smokeComponent()
	source := newCTF(t)
	require.NoError(t, build(t.Context(), source, c))

	base, plainHTTP := pmocm.ParseRepositoryURL(url)
	resolver, err := ociurl.New(ociurl.WithBaseURL(base), ociurl.WithPlainHTTP(plainHTTP))
	require.NoError(t, err)
	target, err := oci.NewRepository(oci.WithResolver(resolver))
	require.NoError(t, err)
	require.NoError(t, transfer(t.Context(), source, target, c))

	read, err := pmocm.New().Resolve(t.Context(), pmocm.OCMRepositorySpec{URL: url}, c.Name, c.Version)
	require.NoError(t, err)

	desc := read.Descriptor()
	require.Len(t, desc.Component.Resources, 1)
	require.Equal(t, "app-manifests", desc.Component.Resources[0].Name)

	// A resource identity carries name and version, so look it up the way
	// pkg/ocmmodule does: by name, through the descriptor.
	blob, err := read.Download(t.Context(), &desc.Component.Resources[0])
	require.NoError(t, err)
	require.Equal(t, c.Resources["app-manifests"], readBlob(t, blob))
}
