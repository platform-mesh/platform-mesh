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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ociurl "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/repository"

	pmocm "go.platform-mesh.io/platform-mesh-deployer/pkg/ocm"
)

// The OCM resource type and media type of a module payload.
const (
	ManifestResourceType = "platformMesh.io/manifests"
	ManifestMediaType    = "application/x-yaml"
)

// Component is a component version to publish: a name, a version and the
// resources keyed by resource name.
type Component struct {
	Name      string
	Version   string
	Resources map[string]string
}

// PublishComponent builds the component version into a CTF archive and
// transfers it into the cluster's registry. The CTF is cached under
// testdata/.cache keyed by the content, so repeated runs only transfer.
//
// The CTF detour mirrors the real packaging pipeline (build once, transfer
// many); the transfer is hand-rolled because there is no ocm CLI in the test
// environment and bindings/go/transfer has no published version yet.
func (e *Env) PublishComponent(t *testing.T, c Component) string {
	t.Helper()

	dir := ctfCache(t, c)
	archive, err := ctf.OpenCTFFromOSPath(dir, os.O_RDWR)
	require.NoError(t, err)
	source, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	require.NoError(t, err)

	url := e.RegistryURL()
	base, plainHTTP := pmocm.ParseRepositoryURL(url)
	resolver, err := ociurl.New(ociurl.WithBaseURL(base), ociurl.WithPlainHTTP(plainHTTP))
	require.NoError(t, err)
	target, err := oci.NewRepository(oci.WithResolver(resolver))
	require.NoError(t, err)

	require.NoError(t, transfer(t.Context(), source, target, c))
	t.Logf("published %s:%s to %s", c.Name, c.Version, url)
	return url
}

// RegistryURL is the address the in-process deployer reaches the cluster's
// registry on. Plain HTTP, so the OCM bindings skip TLS.
func (e *Env) RegistryURL() string {
	return "http://" + e.registryAddr
}

// ctfCache returns a CTF directory holding the component, building it on a
// cache miss. The key covers every resource, so changing a manifest rebuilds.
func ctfCache(t *testing.T, c Component) string {
	t.Helper()

	dir := filepath.Join(testdataDir(), ".cache", "ctf-"+contentHash(c))
	built := filepath.Join(dir, "index.json")
	if _, err := os.Stat(built); err == nil {
		return dir
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))

	archive, err := ctf.OpenCTFFromOSPath(dir, os.O_RDWR|os.O_CREATE)
	require.NoError(t, err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	require.NoError(t, err)
	require.NoError(t, build(t.Context(), repo, c))
	return dir
}

// testdataDir is tests/e2e/deployer/testdata.
func testdataDir() string {
	return filepath.Join(repoRoot(), "tests", "e2e", "deployer", "testdata")
}

// build writes the component version and its resources into repo.
// A resource must carry a local-blob access before its content can be stored,
// and the component version is written afterwards so it records the access the
// store assigned.
func build(ctx context.Context, repo repository.ComponentVersionRepository, c Component) error {
	desc := &descriptorruntime.Descriptor{
		Meta: descriptorruntime.Meta{Version: "v2"},
		Component: descriptorruntime.Component{
			ComponentMeta: descriptorruntime.ComponentMeta{
				ObjectMeta: descriptorruntime.ObjectMeta{Name: c.Name, Version: c.Version},
			},
			Provider: descriptorruntime.Provider{Name: "platform-mesh.io"},
		},
	}

	for _, name := range sortedKeys(c.Resources) {
		content := []byte(c.Resources[name])
		res := &descriptorruntime.Resource{
			ElementMeta: descriptorruntime.ElementMeta{
				ObjectMeta: descriptorruntime.ObjectMeta{Name: name, Version: c.Version},
			},
			Type:     ManifestResourceType,
			Relation: descriptorruntime.LocalRelation,
			Access: &v2.LocalBlob{
				LocalReference: digest.FromBytes(content).String(),
				MediaType:      ManifestMediaType,
			},
		}
		stored, err := repo.AddLocalResource(ctx, c.Name, c.Version, res, inmemory.New(bytes.NewReader(content)))
		if err != nil {
			return fmt.Errorf("adding resource %q: %w", name, err)
		}
		desc.Component.Resources = append(desc.Component.Resources, *stored)
	}

	if err := repo.AddComponentVersion(ctx, desc); err != nil {
		return fmt.Errorf("adding component version: %w", err)
	}
	return nil
}

// transfer copies a component version and its local resources between
// repositories, re-storing each resource so it gains the target's access.
func transfer(ctx context.Context, source, target repository.ComponentVersionRepository, c Component) error {
	desc, err := source.GetComponentVersion(ctx, c.Name, c.Version)
	if err != nil {
		return fmt.Errorf("reading component version: %w", err)
	}

	stored := desc.Component.Resources
	desc.Component.Resources = nil
	for i := range stored {
		res := stored[i]
		data, _, err := source.GetLocalResource(ctx, c.Name, c.Version, res.ToIdentity())
		if err != nil {
			return fmt.Errorf("reading resource %q: %w", res.Name, err)
		}
		written, err := target.AddLocalResource(ctx, c.Name, c.Version, &res, data)
		if err != nil {
			return fmt.Errorf("writing resource %q: %w", res.Name, err)
		}
		desc.Component.Resources = append(desc.Component.Resources, *written)
	}

	if err := target.AddComponentVersion(ctx, desc); err != nil {
		return fmt.Errorf("writing component version: %w", err)
	}
	return nil
}

func contentHash(c Component) string {
	h := sha256.New()
	// Hashing to a sha256 never fails.
	_, _ = fmt.Fprintf(h, "%s\n%s\n", c.Name, c.Version)
	for _, name := range sortedKeys(c.Resources) {
		_, _ = fmt.Fprintf(h, "%s\n%s\n", name, c.Resources[name])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
