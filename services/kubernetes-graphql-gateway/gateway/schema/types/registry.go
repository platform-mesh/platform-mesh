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

package types

import (
	"regexp"
	"sync"

	"github.com/gobuffalo/flect"
	"github.com/graphql-go/graphql"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	invalidGroupCharRegex = regexp.MustCompile(`[^_a-zA-Z0-9]`)
	validGroupStartRegex  = regexp.MustCompile(`^[_a-zA-Z]`)
)

type ConversionState int

const (
	StateNotStarted ConversionState = iota
	StateProcessing
	StateComplete
)

type TypeEntry struct {
	Output *graphql.Object
	Input  *graphql.InputObject
	State  ConversionState
}

type Registry struct {
	mu    sync.RWMutex
	types map[string]*TypeEntry
}

func NewRegistry() *Registry {
	return &Registry{
		types: make(map[string]*TypeEntry),
	}
}

func (r *Registry) Register(key string, output *graphql.Object, input *graphql.InputObject) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.getOrCreateEntry(key)
	entry.Output = output
	entry.Input = input
	entry.State = StateComplete
}

func (r *Registry) Get(key string) (*graphql.Object, *graphql.InputObject) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.types[key]
	if !exists || entry.State != StateComplete {
		return nil, nil
	}
	return entry.Output, entry.Input
}

func (r *Registry) IsProcessing(key string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.types[key]
	return exists && entry.State == StateProcessing
}

func (r *Registry) MarkProcessing(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.getOrCreateEntry(key)
	entry.State = StateProcessing
}

func (r *Registry) UnmarkProcessing(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry, exists := r.types[key]; exists {
		if entry.State == StateProcessing {
			entry.State = StateNotStarted
		}
	}
}

// GetUniqueTypeName returns a unique type name for a GVK, handling conflicts
// when the same Kind exists in different API groups.
// TODO: refactor to always qualify all versions when multi-version resources exist (symmetric naming, no first-wins).
func (r *Registry) GetUniqueTypeName(gvk *schema.GroupVersionKind) string {
	sanitizedGroup := ""
	if gvk.Group != "" {
		sanitizedGroup = SanitizeGroupName(gvk.Group)
	}
	return flect.Pascalize(sanitizedGroup+"_"+gvk.Version) + gvk.Kind
}

// SanitizeGroupName converts a Kubernetes API group name to a valid GraphQL identifier.
// It replaces invalid characters with underscores and ensures the name starts with a letter or underscore.
func SanitizeGroupName(groupName string) string {
	sanitized := invalidGroupCharRegex.ReplaceAllString(groupName, "_")
	if sanitized != "" && !validGroupStartRegex.MatchString(sanitized) {
		sanitized = "_" + sanitized
	}
	return sanitized
}

func (r *Registry) getOrCreateEntry(key string) *TypeEntry {
	entry, exists := r.types[key]
	if !exists {
		entry = &TypeEntry{State: StateNotStarted}
		r.types[key] = entry
	}
	return entry
}
