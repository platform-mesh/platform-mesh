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

// generate-ocm-configuration generates the operator's OCM ConfigMap template.
//
// Usage: generate-ocm-configuration [charts-dir] [output-file]
//
// Run from the repository root. Defaults are charts and
// charts/platform-mesh-operator/templates/ocm.yaml. third_party is also scanned.
// Service enablement comes from ocm.services in the operator chart's values.yaml.
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
)

const (
	defaultChartsDir = "charts"
	defaultOutput    = "charts/platform-mesh-operator/templates/ocm.yaml"
	operatorChart    = "platform-mesh-operator"
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

// run coordinates input loading, rendering, and writing the generated template.
func run(args []string) error {
	// Optional positional arguments override the default input and output paths.
	if len(args) > 2 {
		return errors.New("usage: generate-ocm-configuration [charts-dir] [output-file]")
	}
	chartsDir, output := defaultChartsDir, defaultOutput
	if len(args) > 0 {
		chartsDir = args[0]
	}
	if len(args) > 1 {
		output = args[1]
	}

	// Resolve chart services to their OCM image definitions.
	services, err := loadServices(chartsDir)
	if err != nil {
		return err
	}
	// Complete rendering before replacing the existing generated file.
	data, err := renderConfigMap(services)
	if err != nil {
		return err
	}
	if err := os.WriteFile(output, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", output, err)
	}
	log.Printf("generated OCM configuration for %d services in %s", len(services), output)
	return nil
}
