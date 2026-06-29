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

package config

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

const DefaultNamespace = "platform-mesh-backup-operator"

// DefaultCNPGNamespace is the namespace where CloudNativePG Cluster and Backup CRs live.
const DefaultCNPGNamespace = "platform-mesh-backup-operator"

// DefaultCNPGClusters is empty: the operator discovers CNPG Cluster CRs in CNPGNamespace at runtime.
// Set --cnpg-clusters to override with a static list.
var DefaultCNPGClusters []string

// DefaultVeleroImage is the pinned Velero server/node-agent image.
const DefaultVeleroImage = "velero/velero:v1.18.2"

type OperatorConfig struct {
	Namespace     string
	CNPGNamespace string
	CNPGClusters  []string
	VeleroImage   string
}

func NewOperatorConfig() OperatorConfig {
	return OperatorConfig{
		Namespace:     DefaultNamespace,
		CNPGNamespace: DefaultCNPGNamespace,
		CNPGClusters:  DefaultCNPGClusters,
		VeleroImage:   DefaultVeleroImage,
	}
}

func (c *OperatorConfig) Validate() error {
	c.Namespace = strings.TrimSpace(c.Namespace)
	if c.Namespace == "" {
		return fmt.Errorf("--namespace must not be empty")
	}
	c.CNPGNamespace = strings.TrimSpace(c.CNPGNamespace)
	if c.CNPGNamespace == "" {
		return fmt.Errorf("--cnpg-namespace must not be empty")
	}
	if strings.TrimSpace(c.VeleroImage) == "" {
		c.VeleroImage = DefaultVeleroImage
	}
	return nil
}

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.Namespace, "namespace", c.Namespace, "Namespace in which the operator manages resources")
	fs.StringVar(&c.CNPGNamespace, "cnpg-namespace", c.CNPGNamespace, "Namespace where CloudNativePG Cluster and Backup CRs live")
	fs.StringSliceVar(&c.CNPGClusters, "cnpg-clusters", c.CNPGClusters, "Comma-separated list of CNPG Cluster names to back up and restore")
	fs.StringVar(&c.VeleroImage, "velero-image", c.VeleroImage, "Velero server and node-agent container image (override for air-gapped deployments)")
}
