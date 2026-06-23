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

package analyzer

import "go.platform-mesh.io/qbrtool/internal/models"

// Analyzer is an interface for analyzing project items
type Analyzer interface {
	// Name returns the name of the analyzer
	Name() string
	// Analyze runs the analysis on the given items
	Analyze(items []*models.ProjectItem) *models.AnalysisResult
}

// Registry holds registered analyzers
type Registry struct {
	analyzers map[string]Analyzer
}

// NewRegistry creates a new analyzer registry
func NewRegistry() *Registry {
	return &Registry{
		analyzers: make(map[string]Analyzer),
	}
}

// Register adds an analyzer to the registry
func (r *Registry) Register(a Analyzer) {
	r.analyzers[a.Name()] = a
}

// Get returns an analyzer by name
func (r *Registry) Get(name string) (Analyzer, bool) {
	a, ok := r.analyzers[name]
	return a, ok
}

// All returns all registered analyzers
func (r *Registry) All() []Analyzer {
	result := make([]Analyzer, 0, len(r.analyzers))
	for _, a := range r.analyzers {
		result = append(result, a)
	}
	return result
}

// NewDefaultRegistry creates a registry with all default analyzers
func NewDefaultRegistry(ossOrgs []string) *Registry {
	r := NewRegistry()
	r.Register(NewCVEAnalyzer())
	r.Register(NewOSSAnalyzer(ossOrgs))
	r.Register(NewMonitoringAnalyzer())
	r.Register(NewLifecycleAnalyzer())
	r.Register(NewSecurityAnalyzer())
	return r
}
