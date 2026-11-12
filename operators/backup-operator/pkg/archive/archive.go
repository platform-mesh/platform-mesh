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

// Package archive writes and reads a backup archive: a tar with encrypted entries and the
// manifest as last entry.
package archive

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"go.platform-mesh.io/backup-operator/pkg/crypto"
	"go.platform-mesh.io/backup-operator/pkg/manifest"
)

type Writer struct {
	tw  *tar.Writer
	key []byte
}

func NewWriter(w io.Writer, key []byte) *Writer {
	return &Writer{tw: tar.NewWriter(w), key: key}
}

// Add encrypts size bytes of r into the entry name. It returns the sha256 of the plain data.
func (w *Writer) Add(name string, r io.Reader, size int64) (string, error) {
	err := w.tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o600,
		Size:     crypto.EncryptedSize(size),
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
	})
	if err != nil {
		return "", err
	}
	enc, err := crypto.NewWriter(w.key, w.tw)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(enc, io.TeeReader(io.LimitReader(r, size), h))
	if err != nil {
		return "", fmt.Errorf("writing %s: %w", name, err)
	}
	if n != size {
		return "", fmt.Errorf("writing %s: got %d bytes, want %d", name, n, size)
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Close writes the manifest and ends the archive.
func (w *Writer) Close(m *manifest.Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	err = w.tw.WriteHeader(&tar.Header{Name: manifest.FileName, Mode: 0o600, Size: int64(len(data)), ModTime: time.Now(), Typeflag: tar.TypeReg})
	if err != nil {
		return err
	}
	if _, err := w.tw.Write(data); err != nil {
		return err
	}
	return w.tw.Close()
}

// Extracted is an archive on disk. The entries stay encrypted, Open decrypts them.
type Extracted struct {
	Dir      string
	Manifest *manifest.Manifest
	key      []byte
}

// Extract writes the entries of the archive to dir, then checks that every entry of the manifest
// is there and can be decrypted with key to the same size and sha256.
func Extract(r io.Reader, key []byte, dir string) (*Extracted, error) {
	tr := tar.NewReader(r)
	var mf []byte
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(h.Name)
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("archive entry %q is outside the archive", h.Name)
		}
		if name == manifest.FileName {
			if mf, err = io.ReadAll(tr); err != nil {
				return nil, fmt.Errorf("reading manifest: %w", err)
			}
			continue
		}
		if err := writeFile(filepath.Join(dir, filepath.FromSlash(name)), tr); err != nil {
			return nil, err
		}
	}
	if mf == nil {
		return nil, errors.New("archive has no manifest, the backup is not complete")
	}
	m, err := manifest.Parse(mf)
	if err != nil {
		return nil, err
	}
	x := &Extracted{Dir: dir, Manifest: m, key: key}
	if err := x.check(manifest.PKIEntry, m.PKI.Size, m.PKI.SHA256); err != nil {
		return nil, err
	}
	for _, e := range m.Etcds {
		if err := x.check(e.Entry(), e.Size, e.SHA256); err != nil {
			return nil, err
		}
	}
	return x, nil
}

func writeFile(name string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return fmt.Errorf("extracting %s: %w", name, err)
	}
	return f.Close()
}

func (x *Extracted) check(name string, size int64, sum string) error {
	r, err := x.Open(name)
	if err != nil {
		return err
	}
	defer r.Close() //nolint:errcheck
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != sum {
		return fmt.Errorf("%s does not match the manifest", name)
	}
	return nil
}

// Open returns the decrypted entry.
func (x *Extracted) Open(name string) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(x.Dir, filepath.FromSlash(name)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("archive has no entry %s", name)
	}
	if err != nil {
		return nil, err
	}
	r, err := crypto.NewReader(x.key, f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return struct {
		io.Reader
		io.Closer
	}{r, f}, nil
}

// ReadAll returns the decrypted entry.
func (x *Extracted) ReadAll(name string) ([]byte, error) {
	r, err := x.Open(name)
	if err != nil {
		return nil, err
	}
	defer r.Close() //nolint:errcheck
	return io.ReadAll(r)
}
