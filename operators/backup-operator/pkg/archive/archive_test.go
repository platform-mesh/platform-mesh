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

package archive_test

import (
	"archive/tar"
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/backup-operator/pkg/archive"
	"go.platform-mesh.io/backup-operator/pkg/manifest"
)

var key = []byte("0123456789abcdef0123456789abcdef")

func build(t *testing.T, snapshot string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := archive.NewWriter(&buf, key)
	m := &manifest.Manifest{Version: manifest.Version, Backup: "b1", Namespace: "ns"}

	pkiData := []byte(`{"secrets":[]}`)
	sum, err := w.Add(manifest.PKIEntry, bytes.NewReader(pkiData), int64(len(pkiData)))
	require.NoError(t, err)
	m.PKI = manifest.PKI{Size: int64(len(pkiData)), SHA256: sum}

	e := manifest.Etcd{Name: "etcd-kcp", Snapshot: "Full-00000000-00000100-1", Size: int64(len(snapshot))}
	e.SHA256, err = w.Add(e.Entry(), strings.NewReader(snapshot), e.Size)
	require.NoError(t, err)
	m.Etcds = append(m.Etcds, e)
	require.NoError(t, w.Close(m))
	return buf.Bytes()
}

func TestRoundTrip(t *testing.T) {
	data := build(t, "snapshot data")
	assert.NotContains(t, string(data), "snapshot data", "entries are encrypted")

	x, err := archive.Extract(bytes.NewReader(data), key, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "b1", x.Manifest.Backup)
	got, err := x.ReadAll(x.Manifest.Etcds[0].Entry())
	require.NoError(t, err)
	assert.Equal(t, "snapshot data", string(got))
}

func TestAddChecksSize(t *testing.T) {
	w := archive.NewWriter(&bytes.Buffer{}, key)
	_, err := w.Add("x", strings.NewReader("abc"), 5)
	assert.ErrorContains(t, err, "got 3 bytes, want 5")
}

func TestExtractFails(t *testing.T) {
	_, err := archive.Extract(bytes.NewReader(build(t, "data")), []byte("ffffffffffffffffffffffffffffffff"), t.TempDir())
	assert.ErrorContains(t, err, "wrong key or broken data")

	changed := build(t, "data")
	i := bytes.Index(changed, []byte("etcd/etcd-kcp/"))
	require.Positive(t, i)
	// the entry data starts at the next 512 byte block after its header
	changed[(i/512+1)*512+10] ^= 1
	_, err = archive.Extract(bytes.NewReader(changed), key, t.TempDir())
	assert.ErrorContains(t, err, "wrong key or broken data")

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "pki.json.enc", Size: 1, Typeflag: tar.TypeReg}))
	_, _ = tw.Write([]byte("x"))
	require.NoError(t, tw.Close())
	_, err = archive.Extract(bytes.NewReader(buf.Bytes()), key, t.TempDir())
	assert.ErrorContains(t, err, "no manifest, the backup is not complete")

	buf.Reset()
	tw = tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "../evil", Size: 1, Typeflag: tar.TypeReg}))
	_, _ = tw.Write([]byte("x"))
	require.NoError(t, tw.Close())
	_, err = archive.Extract(bytes.NewReader(buf.Bytes()), key, t.TempDir())
	assert.ErrorContains(t, err, "outside the archive")
}
