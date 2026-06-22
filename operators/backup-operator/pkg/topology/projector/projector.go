package projector

import (
	"context"
	"fmt"

	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.platform-mesh.io/backup-operator/pkg/topology"
)

const configMapName = "backup-topology-schemas"

// Projector ensures the topology schema ConfigMap is present and up-to-date.
type Projector struct {
	client    client.Client
	namespace string
}

// New returns a Projector that manages the schema ConfigMap in namespace.
func New(c client.Client, namespace string) *Projector {
	return &Projector{client: c, namespace: namespace}
}

// EnsureConfigMap creates or updates the backup-topology-schemas ConfigMap with
// the current schema(s) keyed by schemaVersion. Idempotent via server-side apply.
func (p *Projector) EnsureConfigMap(ctx context.Context) error {
	schemaData, err := topology.SchemaV1Alpha1()
	if err != nil {
		return fmt.Errorf("reading topology schema: %w", err)
	}

	cm := corev1apply.ConfigMap(configMapName, p.namespace).
		WithData(map[string]string{
			"v1alpha1.json": string(schemaData),
		})

	if err := p.client.Apply(ctx, cm, client.FieldOwner("backup-operator"), client.ForceOwnership); err != nil {
		return fmt.Errorf("applying topology schema ConfigMap: %w", err)
	}
	return nil
}
