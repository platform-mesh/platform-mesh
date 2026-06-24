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
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"

	_ "embed"
)

//go:embed schema/v1alpha1.json
var schemaV1alpha1 []byte

// ValidationError is returned when a document fails JSON Schema validation.
type ValidationError struct {
	SchemaErrors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("topology validation failed: %s", strings.Join(e.SchemaErrors, "; "))
}

// FieldMismatch describes a single divergent field between source and target.
type FieldMismatch struct {
	Field  string
	Source string
	Target string
}

// MismatchError is returned by Validate when source and target topologies differ.
type MismatchError struct {
	Fields []FieldMismatch
}

func (e *MismatchError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = fmt.Sprintf("%s: source=%q target=%q", f.Field, f.Source, f.Target)
	}
	return fmt.Sprintf("topology mismatch: %s", strings.Join(parts, "; "))
}

// Marshal serialises m to JSON and validates it against the v1alpha1 schema.
func Marshal(m *Manifest) ([]byte, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshalling topology: %w", err)
	}
	if err := validate(data); err != nil {
		return nil, err
	}
	return data, nil
}

// Unmarshal parses data as JSON, validates against the v1alpha1 schema, and
// populates m. Returns *ValidationError if the document is invalid.
func Unmarshal(data []byte, m *Manifest) error {
	if err := validate(data); err != nil {
		return err
	}
	if err := json.Unmarshal(data, m); err != nil {
		return fmt.Errorf("unmarshalling topology: %w", err)
	}
	return nil
}

// Validate compares source (from S3) against target (from the live cluster)
// under Strict mode. Returns *MismatchError listing each divergent field, or
// nil if equal.
func Validate(source, target *Manifest) error {
	var mismatches []FieldMismatch

	if source.HostCluster.KubernetesVersion != target.HostCluster.KubernetesVersion {
		mismatches = append(mismatches, FieldMismatch{
			Field:  "hostCluster.kubernetesVersion",
			Source: source.HostCluster.KubernetesVersion,
			Target: target.HostCluster.KubernetesVersion,
		})
	}
	if source.HostCluster.Namespace != target.HostCluster.Namespace {
		mismatches = append(mismatches, FieldMismatch{
			Field:  "hostCluster.namespace",
			Source: source.HostCluster.Namespace,
			Target: target.HostCluster.Namespace,
		})
	}

	// Compare KCP shards by name (order-independent).
	sourceShards := shardsByName(source.Kcp.Shards)
	targetShards := shardsByName(target.Kcp.Shards)

	for name, ss := range sourceShards {
		ts, ok := targetShards[name]
		if !ok {
			mismatches = append(mismatches, FieldMismatch{
				Field:  fmt.Sprintf("kcp.shards[%s]", name),
				Source: name,
				Target: "<missing>",
			})
			continue
		}
		if ss.LogicalClusterIDsDigest != ts.LogicalClusterIDsDigest {
			mismatches = append(mismatches, FieldMismatch{
				Field:  fmt.Sprintf("kcp.shards[%s].logicalClusterIDsDigest", name),
				Source: ss.LogicalClusterIDsDigest,
				Target: ts.LogicalClusterIDsDigest,
			})
		}
		if ss.EtcdRef != ts.EtcdRef {
			mismatches = append(mismatches, FieldMismatch{
				Field:  fmt.Sprintf("kcp.shards[%s].etcdRef", name),
				Source: ss.EtcdRef,
				Target: ts.EtcdRef,
			})
		}
	}
	for name := range targetShards {
		if _, ok := sourceShards[name]; !ok {
			mismatches = append(mismatches, FieldMismatch{
				Field:  fmt.Sprintf("kcp.shards[%s]", name),
				Source: "<missing>",
				Target: name,
			})
		}
	}

	// Compare CNPG clusters by name.
	sourceClusters := cnpgClustersByName(source.CNPG.Clusters)
	targetClusters := cnpgClustersByName(target.CNPG.Clusters)
	for name, sc := range sourceClusters {
		tc, ok := targetClusters[name]
		if !ok {
			mismatches = append(mismatches, FieldMismatch{
				Field:  fmt.Sprintf("cnpg.clusters[%s].specDigest", name),
				Source: sc.SpecDigest,
				Target: "<missing>",
			})
			continue
		}
		if sc.SpecDigest != tc.SpecDigest {
			mismatches = append(mismatches, FieldMismatch{
				Field:  fmt.Sprintf("cnpg.clusters[%s].specDigest", name),
				Source: sc.SpecDigest,
				Target: tc.SpecDigest,
			})
		}
	}

	// Compare OpenFGA stores by name.
	sourceStores := openfgaStoresByName(source.OpenFGA.Stores)
	targetStores := openfgaStoresByName(target.OpenFGA.Stores)
	for name, ss := range sourceStores {
		ts, ok := targetStores[name]
		if !ok {
			mismatches = append(mismatches, FieldMismatch{
				Field:  fmt.Sprintf("openfga.stores[%s].modelDigest", name),
				Source: ss.ModelDigest,
				Target: "<missing>",
			})
			continue
		}
		if ss.ModelDigest != ts.ModelDigest {
			mismatches = append(mismatches, FieldMismatch{
				Field:  fmt.Sprintf("openfga.stores[%s].modelDigest", name),
				Source: ss.ModelDigest,
				Target: ts.ModelDigest,
			})
		}
	}

	if len(mismatches) > 0 {
		return &MismatchError{Fields: mismatches}
	}
	return nil
}

// Digest returns the SHA-256 hex digest of the canonical JSON of m.
func Digest(m *Manifest) (string, error) {
	canonical, err := canonicalJSON(m)
	if err != nil {
		return "", fmt.Errorf("computing topology digest: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return fmt.Sprintf("sha256:%x", sum), nil
}

// SchemaV1Alpha1 returns the raw JSON Schema bytes for topology v1alpha1.
func SchemaV1Alpha1() ([]byte, error) {
	out := make([]byte, len(schemaV1alpha1))
	copy(out, schemaV1alpha1)
	return out, nil
}

// validate runs JSON Schema validation on raw JSON bytes.
func validate(data []byte) error {
	schemaLoader := gojsonschema.NewBytesLoader(schemaV1alpha1)
	docLoader := gojsonschema.NewBytesLoader(data)

	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		return &ValidationError{SchemaErrors: []string{err.Error()}}
	}
	if !result.Valid() {
		errs := make([]string, len(result.Errors()))
		for i, e := range result.Errors() {
			errs[i] = e.String()
		}
		return &ValidationError{SchemaErrors: errs}
	}
	return nil
}

// canonicalJSON produces deterministic JSON with sorted keys.
func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	// Round-trip through map to sort keys.
	var m any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func shardsByName(shards []KcpShard) map[string]KcpShard {
	m := make(map[string]KcpShard, len(shards))
	for _, s := range shards {
		m[s.Name] = s
	}
	return m
}

func cnpgClustersByName(clusters []CNPGCluster) map[string]CNPGCluster {
	m := make(map[string]CNPGCluster, len(clusters))
	for _, c := range clusters {
		m[c.Name] = c
	}
	return m
}

func openfgaStoresByName(stores []OpenFGAStore) map[string]OpenFGAStore {
	m := make(map[string]OpenFGAStore, len(stores))
	for _, s := range stores {
		m[s.Name] = s
	}
	return m
}
