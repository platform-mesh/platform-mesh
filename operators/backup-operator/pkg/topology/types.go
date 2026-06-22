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

package topology

import (
	"time"
)

// Manifest is the in-memory representation of topology.json.
type Manifest struct {
	SchemaVersion   string          `json:"schemaVersion"`
	CapturedAt      time.Time       `json:"capturedAt"`
	HostCluster     HostCluster     `json:"hostCluster"`
	Kcp             KcpTopology     `json:"kcp"`
	CNPG            CNPGTopology    `json:"cnpg"`
	OpenFGA         OpenFGATopology `json:"openfga"`
	OperatorVersion string          `json:"operatorVersion"`
}

type HostCluster struct {
	KubernetesVersion string `json:"kubernetesVersion"`
	Namespace         string `json:"namespace"`
}

type KcpTopology struct {
	ShardCount int        `json:"shardCount"`
	Shards     []KcpShard `json:"shards"`
}

type KcpShard struct {
	Name                    string `json:"name"`
	EtcdRef                 string `json:"etcdRef"`
	LogicalClusterIDsDigest string `json:"logicalClusterIDsDigest"`
}

type CNPGTopology struct {
	Clusters []CNPGCluster `json:"clusters"`
}

type CNPGCluster struct {
	Name         string `json:"name"`
	SpecDigest   string `json:"specDigest"`
	MajorVersion int    `json:"majorVersion"`
}

type OpenFGATopology struct {
	Stores []OpenFGAStore `json:"stores"`
}

type OpenFGAStore struct {
	Name        string `json:"name"`
	ModelDigest string `json:"modelDigest"`
}
