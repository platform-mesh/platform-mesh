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

// Package composition wraps kro's graph engine (used as a library) to compile a
// ResourceGraphDefinition into its generated CRD and resource graph.
package composition

import (
	"fmt"

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/graph"

	"k8s.io/client-go/rest"
)

// Collection-expansion limits, mirroring kro's controller defaults. The dimension cap is
// checked before any expansion runs, gating nested cartesian products cheaply.
const (
	maxCollectionSize          = 1000
	maxCollectionDimensionSize = 10
)

// RGDConfig is shared so compile and reconcile use the same limits.
func RGDConfig() graph.Config {
	return graph.Config{
		MaxCollectionSize:          maxCollectionSize,
		MaxCollectionDimensionSize: maxCollectionDimensionSize,
	}
}

// Compiler builds kro graphs for one workspace. The rest.Config must point at the
// workspace whose served APIs the RGD composes, because kro's
// builder resolves the RGD's referenced resources via discovery against it.
type Compiler struct {
	builder *graph.Builder
}

// NewCompiler constructs a Compiler bound to the given workspace rest.Config.
func NewCompiler(cfg *rest.Config) (*Compiler, error) {
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("http client: %w", err)
	}
	b, err := graph.NewBuilder(cfg, httpClient)
	if err != nil {
		return nil, fmt.Errorf("graph builder: %w", err)
	}
	return &Compiler{builder: b}, nil
}

// Compile turns an RGD into its processed graph. Fails if it references APIs the target
// workspace does not serve.
func (c *Compiler) Compile(rgd *krov1alpha1.ResourceGraphDefinition) (*graph.Graph, error) {
	g, err := c.builder.NewResourceGraphDefinition(rgd, RGDConfig())
	if err != nil {
		return nil, fmt.Errorf("compile RGD %q: %w", rgd.Name, err)
	}
	return g, nil
}
