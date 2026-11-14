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

// Package manifest is the index file of one backup archive. It is the last entry of the archive.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
)

const (
	Version = 2
	// FileName is the manifest entry, written last.
	FileName = "manifest.json"
	// PKIEntry is the encrypted PKI bundle.
	PKIEntry = "pki.json.enc"
)

type Manifest struct {
	Version   int       `json:"version"`
	Backup    string    `json:"backup"`
	Namespace string    `json:"namespace"`
	CreatedAt time.Time `json:"createdAt"`
	Topology  Topology  `json:"topology"`
	Etcds     []Etcd    `json:"etcds"`
	PKI       PKI       `json:"pki"`
}

// Topology is the kcp setup at backup time. A restore needs the same one.
type Topology struct {
	RootShards   []string `json:"rootShards"`
	Shards       []string `json:"shards"`
	CacheServers []string `json:"cacheServers"`
	FrontProxies []string `json:"frontProxies"`
}

// Etcd is the snapshot of one etcd. Size and SHA256 are of the data before encryption.
type Etcd struct {
	Name        string `json:"name"`
	StorePrefix string `json:"storePrefix"`
	// Snapshot is the druid file name, for example Full-00000000-00000479-1793357962.
	Snapshot string `json:"snapshot"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// Entry is the name of the snapshot in the archive.
func (e Etcd) Entry() string {
	return path.Join("etcd", e.Name, e.Snapshot) + ".enc"
}

type PKI struct {
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Secrets int    `json:"secrets"`
	// Certificates counts the Certificates and Issuers.
	Certificates int `json:"certificates"`
}

// ArchiveKey is the key of a backup archive in the bucket.
func ArchiveKey(prefix, namespace, backup string) string {
	return strings.TrimPrefix(path.Join(prefix, namespace, backup+".tar"), "/")
}

func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func Parse(data []byte) (*Manifest, error) {
	m := &Manifest{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if m.Version != Version {
		return nil, fmt.Errorf("manifest version %d is not supported", m.Version)
	}
	return m, nil
}
