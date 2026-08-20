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

package projector

import (
	"context"
	"fmt"

	"go.platform-mesh.io/backup-operator/pkg/topology"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const configMapName = "backup-topology-schemas"

// Projector ensures the topology schema ConfigMap is present and up-to-date.
type Projector struct {
	client    ctrlruntimeclient.Client
	namespace string
}

// New returns a Projector that manages the schema ConfigMap in namespace.
func New(c ctrlruntimeclient.Client, namespace string) *Projector {
	return &Projector{client: c, namespace: namespace}
}

// EnsureConfigMap creates or updates the backup-topology-schemas ConfigMap with
// the current schema(s) keyed by schemaVersion. Idempotent via server-side apply.
func (p *Projector) EnsureConfigMap(ctx context.Context) error {
	schemaData, err := topology.SchemaV1Alpha1()
	if err != nil {
		return fmt.Errorf("reading topology schema: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: p.namespace,
		},
	}

	_, err = ctrl.CreateOrUpdate(ctx, p.client, cm, func() error {
		cm.Data = map[string]string{
			"v1alpha1.json": string(schemaData),
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("ensuring topology schema ConfigMap: %w", err)
	}

	return nil
}
