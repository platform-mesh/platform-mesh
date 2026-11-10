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

// Package features defines the pm-openfga-operator feature gates, set with --feature-gates=<Gate>=<bool>.
package features

import (
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
)

// OpenFGA enables authorization management.
const OpenFGA featuregate.Feature = "OpenFGA"

var defaultFeatures = map[featuregate.Feature]featuregate.FeatureSpec{
	OpenFGA: {Default: true, PreRelease: featuregate.Alpha},
}

// New returns a feature gate with every pm-openfga-operator feature registered at its default.
func New() featuregate.MutableFeatureGate {
	gate := featuregate.NewFeatureGate()
	utilruntime.Must(gate.Add(defaultFeatures))
	return gate
}
