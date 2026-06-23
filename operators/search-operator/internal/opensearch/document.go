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

package opensearch

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// DefaultIndexMapping returns the default OpenSearch index mapping for workspace and resource documents.
// - payload_raw is stored but not indexed (enabled=false).
// - payload_text stores the full serialized object for full-text search.
func DefaultIndexMapping(semanticFields []string, semanticModelID string) (string, error) {
	properties := map[string]any{
		"id": map[string]any{"type": "keyword"},
		"name": map[string]any{
			"type": "text",
			"fields": map[string]any{
				"keyword": map[string]any{"type": "keyword", "ignore_above": 256},
			},
		},
		"type":              map[string]any{"type": "keyword"},
		"kind":              map[string]any{"type": "keyword"},
		"namespace":         map[string]any{"type": "keyword"},
		"api_group":         map[string]any{"type": "keyword"},
		"api_version":       map[string]any{"type": "keyword"},
		"cluster_name":      map[string]any{"type": "keyword"},
		"path":              map[string]any{"type": "keyword"},
		"workspace_path":    map[string]any{"type": "keyword"},
		"organization_id":   map[string]any{"type": "keyword"},
		"organization_name": map[string]any{"type": "keyword"},
		"account_id":        map[string]any{"type": "keyword"},
		"account_name":      map[string]any{"type": "keyword"},
		"fga_object":        map[string]any{"type": "keyword"},
		"labels":            map[string]any{"type": "flat_object"},
		"annotations":       map[string]any{"type": "flat_object"},
		"permissions": map[string]any{
			"type": "nested",
			"properties": map[string]any{
				"user":     map[string]any{"type": "keyword"},
				"relation": map[string]any{"type": "keyword"},
				"object":   map[string]any{"type": "keyword"},
			},
		},
		"created_at":       map[string]any{"type": "date"},
		"updated_at":       map[string]any{"type": "date"},
		"payload_raw_json": map[string]any{"type": "keyword", "index": false, "doc_values": false},
		"payload_text":     map[string]any{"type": "text"},
	}

	if len(semanticFields) > 0 {
		semanticModelID = strings.TrimSpace(semanticModelID)
		if semanticModelID == "" {
			return "", fmt.Errorf("semantic model id is required when semantic fields are configured")
		}
		for _, fieldPath := range semanticFields {
			if err := addSemanticFieldMapping(properties, fieldPath, semanticModelID); err != nil {
				return "", err
			}
		}
	}

	mapping := map[string]any{
		"dynamic":    false,
		"properties": properties,
	}

	raw, err := json.Marshal(mapping)
	if err != nil {
		return "", fmt.Errorf("marshal index mapping: %w", err)
	}

	return string(raw), nil
}

func addSemanticFieldMapping(properties map[string]any, fieldPath, semanticModelID string) error {
	segments := splitFieldPath(fieldPath)
	if len(segments) == 0 {
		return nil
	}

	current := properties
	for i, segment := range segments {
		existing, exists := current[segment]
		isLeaf := i == len(segments)-1

		if isLeaf {
			if exists {
				existingMap, ok := existing.(map[string]any)
				if !ok {
					return fmt.Errorf("semantic field %q conflicts with existing non-object mapping", fieldPath)
				}
				if existingType, _ := existingMap["type"].(string); existingType != "" && existingType != "semantic" {
					return fmt.Errorf("semantic field %q conflicts with existing %q mapping", fieldPath, existingType)
				}
			}
			current[segment] = map[string]any{
				"type":     "semantic",
				"model_id": semanticModelID,
			}
			return nil
		}

		if !exists {
			next := map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
			current[segment] = next
			current = next["properties"].(map[string]any)
			continue
		}

		existingMap, ok := existing.(map[string]any)
		if !ok {
			return fmt.Errorf("semantic field %q conflicts with existing non-object mapping at %q", fieldPath, segment)
		}
		if existingType, _ := existingMap["type"].(string); existingType != "" && existingType != "object" {
			return fmt.Errorf("semantic field %q conflicts with existing %q mapping at %q", fieldPath, existingType, segment)
		}

		nextProps, ok := existingMap["properties"].(map[string]any)
		if !ok {
			nextProps = map[string]any{}
			existingMap["properties"] = nextProps
		}
		current = nextProps
	}

	return nil
}

func splitFieldPath(fieldPath string) []string {
	rawSegments := strings.Split(strings.TrimSpace(fieldPath), ".")
	segments := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		segments = append(segments, segment)
	}
	return segments
}

// WorkspaceDocument represents an indexed workspace/account in OpenSearch
type WorkspaceDocument struct {
	// Core workspace/account fields
	ID   string `json:"id"`   // Document ID (typically the cluster name)
	Name string `json:"name"` // Human-readable name
	Type string `json:"type"` // "workspace", "account", or "organization"

	// kcp-specific fields
	ClusterName string `json:"cluster_name"` // Logical cluster name
	Path        string `json:"path"`         // Full path in the kcp hierarchy

	// Organization context (for permission scoping)
	OrganizationID   string `json:"organization_id,omitempty"`
	OrganizationName string `json:"organization_name,omitempty"`

	// Account context (if applicable)
	AccountID   string `json:"account_id,omitempty"`
	AccountName string `json:"account_name,omitempty"`

	// FGAObject is the unique FGA object name for this document (e.g. "core_platform-mesh_io_account:ID/name")
	FGAObject string `json:"fga_object,omitempty"`

	// OpenFGA Permission Tuples for this resource
	Permissions []PermissionTuple `json:"permissions,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`

	// Additional metadata
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// PermissionTuple represents an OpenFGA tuple embedded in the document
// This allows for permission-based filtering at search time
type PermissionTuple struct {
	// User is the subject of the permission (e.g., "user:alice" or "role:admin#assignee")
	User string `json:"user"`
	// Relation is the permission type (e.g., "member", "owner", "viewer")
	Relation string `json:"relation"`
	// Object is the target object (typically matches the document ID)
	Object string `json:"object"`
}

// ResourceDocument represents a generic Kubernetes resource indexed in OpenSearch
type ResourceDocument struct {
	// Core resource identification
	ID        string `json:"id"`        // Unique document ID
	Kind      string `json:"kind"`      // Resource kind
	Name      string `json:"name"`      // Resource name
	Namespace string `json:"namespace"` // Resource namespace

	// API version info
	APIGroup   string `json:"api_group"`
	APIVersion string `json:"api_version"`

	// PM context
	ClusterName      string `json:"cluster_name"`
	WorkspacePath    string `json:"workspace_path"`
	OrganizationID   string `json:"organization_id,omitempty"`
	OrganizationName string `json:"organization_name,omitempty"`
	AccountID        string `json:"account_id,omitempty"`
	AccountName      string `json:"account_name,omitempty"`

	// FGAObject is the unique FGA object name for this document
	FGAObject string `json:"fga_object,omitempty"`

	// OpenFGA Permission Tuples for this resource
	Permissions []PermissionTuple `json:"permissions,omitempty"`

	// Resource metadata
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`

	// Resource spec and status (arbitrary nested maps from the unstructured object)
	Spec   map[string]interface{} `json:"spec,omitempty"`
	Status map[string]interface{} `json:"status,omitempty"`

	// CustomFields holds fields from the unstructured resource that are listed in
	// the SearchIndex's DefaultFields. These are propagated directly from the resource.
	CustomFields map[string]any `json:"custom_fields,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`

	// Full raw object payload serialized as JSON, stored but not indexed.
	PayloadRawJSON string `json:"payload_raw_json,omitempty"`

	// Full serialized object payload for full-text search.
	PayloadText string `json:"payload_text,omitempty"`
}

// NewWorkspaceDocument creates a new workspace document with default values
func NewWorkspaceDocument(id, name, workspaceType, clusterName, path string) *WorkspaceDocument {
	return &WorkspaceDocument{
		ID:          id,
		Name:        name,
		Type:        workspaceType,
		ClusterName: clusterName,
		Path:        path,
		UpdatedAt:   time.Now(),
	}
}

// AddPermission adds a permission tuple to the document
func (d *WorkspaceDocument) AddPermission(user, relation, object string) {
	d.Permissions = append(d.Permissions, PermissionTuple{
		User:     user,
		Relation: relation,
		Object:   object,
	})
}

// NewResourceDocument creates a new resource document with default values
func NewResourceDocument(id, kind, name, namespace string, clusterName multicluster.ClusterName, workspacePath string) *ResourceDocument {
	return &ResourceDocument{
		ID:            id,
		Kind:          kind,
		Name:          name,
		Namespace:     namespace,
		ClusterName:   string(clusterName),
		WorkspacePath: workspacePath,
		UpdatedAt:     time.Now(),
	}
}

// AddPermission adds a permission tuple to the resource document
func (d *ResourceDocument) AddPermission(user, relation, object string) {
	d.Permissions = append(d.Permissions, PermissionTuple{
		User:     user,
		Relation: relation,
		Object:   object,
	})
}
