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

// render-constructor writes the root component-constructor.yaml with its
// placeholders filled in, as it is attached to a GitHub release.
//
// Usage: render-constructor <output file>
//
// Run from the repository root. The result is the constructor
// ocm-build-aggregate builds for a release, with the values ocm would expand
// from the environment written out.
//
// Environment:
//
//	PM_VERSION  required; the component version
//	REPO_URL    git repository recorded as the component's source
//	COMMIT      git commit recorded as the component's source
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

const constructor = "component-constructor.yaml"

// placeholder is a ${VAR} reference, the only form the constructors use.
var placeholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func main() {
	log.SetFlags(0)
	log.SetPrefix("error: ")
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	if len(os.Args) != 2 {
		return errors.New("usage: render-constructor <output file>")
	}
	output := os.Args[1]

	version := os.Getenv("PM_VERSION")
	if version == "" {
		return errors.New("PM_VERSION must be set")
	}
	commit, err := releaseutils.Commit(ctx)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(constructor)
	if err != nil {
		return err
	}
	rendered, err := render(string(data), map[string]string{
		"PM_VERSION": version,
		"REPO_URL":   releaseutils.EnvOr("REPO_URL", releaseutils.DefaultRepoURL),
		"COMMIT":     commit,
	})
	if err != nil {
		return fmt.Errorf("rendering %s: %w", constructor, err)
	}

	header := fmt.Sprintf("# GENERATED from %s for github.com/platform-mesh/platform-mesh:%s at %s. Do not edit.\n", constructor, version, commit)
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	return os.WriteFile(output, []byte(header+rendered), 0o644)
}

// render replaces every ${VAR} in text with its value. A placeholder without
// a value is an error: it would be published as it stands.
func render(text string, values map[string]string) (string, error) {
	var unknown []string
	rendered := placeholder.ReplaceAllStringFunc(text, func(match string) string {
		name := placeholder.FindStringSubmatch(match)[1]
		value, ok := values[name]
		if !ok {
			if !slices.Contains(unknown, name) {
				unknown = append(unknown, name)
			}
			return match
		}
		return value
	})
	if len(unknown) > 0 {
		return "", fmt.Errorf("no value for %s", strings.Join(unknown, ", "))
	}
	return rendered, nil
}
