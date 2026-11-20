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

package v1alpha1

import (
	"path"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManifestKind is the kind of the Manifest.
const ManifestKind = "Manifest"

// Manifest is the index of one backup archive. It is the last entry of the archive and not
// encrypted. Sizes and sha256 sums are of the entries before encryption.
type Manifest struct {
	metav1.TypeMeta `json:",inline"`

	Backup    string      `json:"backup"`
	Namespace string      `json:"namespace"`
	CreatedAt metav1.Time `json:"createdAt"`
	Topology  Topology    `json:"topology"`
	Etcds     []Etcd      `json:"etcds"`
	PKI       PKI         `json:"pki"`
}

// Topology is the kcp setup at backup time. A restore needs the same one.
type Topology struct {
	RootShards   []string `json:"rootShards"`
	Shards       []string `json:"shards"`
	CacheServers []string `json:"cacheServers"`
	FrontProxies []string `json:"frontProxies"`
}

// Etcd is the snapshot of one etcd-druid Etcd.
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

// PKI is the entry with the Bundle.
type PKI struct {
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Secrets int    `json:"secrets"`
	// Certificates counts the Certificates and Issuers.
	Certificates int `json:"certificates"`
}

// NewManifest returns a Manifest with apiVersion and kind set.
func NewManifest() *Manifest {
	return &Manifest{TypeMeta: metav1.TypeMeta{APIVersion: SchemeGroupVersion.String(), Kind: ManifestKind}}
}
