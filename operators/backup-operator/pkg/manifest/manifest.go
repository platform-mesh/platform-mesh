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

// Package manifest has the entry names and helpers for the manifest, the index of a backup archive.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	archivev1alpha1 "go.platform-mesh.io/apis/backup/archive/v1alpha1"
)

const (
	// FileName is the manifest entry, written last.
	FileName = "manifest.json"
	// PKIEntry is the encrypted PKI bundle.
	PKIEntry = "pki.json.enc"
)

// ArchiveKey is the key of a backup archive in the bucket.
func ArchiveKey(prefix, namespace, backup string) string {
	return strings.TrimPrefix(path.Join(prefix, namespace, backup+".tar"), "/")
}

func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func Parse(data []byte) (*archivev1alpha1.Manifest, error) {
	m := &archivev1alpha1.Manifest{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if m.APIVersion != archivev1alpha1.SchemeGroupVersion.String() || m.Kind != archivev1alpha1.ManifestKind {
		return nil, fmt.Errorf("manifest %s %s is not supported", m.APIVersion, m.Kind)
	}
	return m, nil
}
