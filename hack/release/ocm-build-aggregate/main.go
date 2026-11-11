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

// ocm-build-aggregate publishes the root Platform Mesh component version.
//
// Usage: ocm-build-aggregate
//
// Run from the repository root. ocm comes from PATH.
//
// Environment:
//
//	PM_VERSION  required; the component version
//	OCM_REPO    target repository, ctf::<path> builds into an archive instead
//	REPO_URL    git repository recorded as the component's source
//	COMMIT      git commit recorded as the component's source
//	SIGNATURE   sign the component versions with this signature
//	SKIP_COMPONENTS  optional components (ocm/components.yaml) to leave out
//	            of the aggregate, separated by commas
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"go.yaml.in/yaml/v3"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

const (
	constructor = "component-constructor.yaml"

	etcdDruid     = "github.com/gardener/etcd-druid"
	etcdDruidRepo = "europe-docker.pkg.dev/gardener-project/releases"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix(">>> ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		log.SetPrefix("error: ")
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	if len(os.Args) != 1 {
		return errors.New("usage: ocm-build-aggregate")
	}

	version := os.Getenv("PM_VERSION")
	if version == "" {
		return errors.New("PM_VERSION must be set")
	}

	ocmRepo := releaseutils.OCMRepo()

	commit, err := releaseutils.Commit(ctx)
	if err != nil {
		return err
	}

	env := map[string]string{
		"PM_VERSION": version,
		"REPO_URL":   releaseutils.EnvOr("REPO_URL", releaseutils.DefaultRepoURL),
		"COMMIT":     commit,
	}

	repo := releaseutils.OCMRepository{
		Repo:      ocmRepo,
		Signature: os.Getenv("SIGNATURE"),
	}

	if err := copyEtcdDruid(ctx, repo); err != nil {
		return err
	}

	skipped, err := releaseutils.SkippedComponents()
	if err != nil {
		return err
	}
	selected, dropped, cleanup, err := selectedConstructor(skipped)
	if err != nil {
		return err
	}
	defer cleanup()
	if len(dropped) > 0 {
		log.Printf("leaving out %s", strings.Join(dropped, ", "))
	}

	log.Printf("building %s into %s", constructor, ocmRepo)
	cvs, err := repo.Add(ctx, selected, "", env)
	if err != nil {
		return err
	}
	if err := repo.Sign(ctx, cvs); err != nil {
		return err
	}

	log.Printf("done: %s", strings.Join(cvs, " "))
	return nil
}

// copyEtcdDruid mirrors etcd-druid at the version the aggregate references into repo.
func copyEtcdDruid(ctx context.Context, repo releaseutils.OCMRepository) error {
	version, err := referencedVersion(etcdDruid)
	if err != nil {
		return err
	}
	if version == "" {
		log.Printf("%s references no %s", constructor, etcdDruid)
		return nil
	}
	cv := etcdDruid + ":" + version

	// when copying from registry to registry ocm overwrites silently,
	// so check first if etcd-druid is already present.
	exists, err := repo.Exists(ctx, cv)
	if err != nil {
		return err
	}
	if exists {
		log.Printf("%s already present", cv)
		return nil
	}

	log.Printf("copying %s from %s", cv, etcdDruidRepo)
	source := releaseutils.OCMRepository{Repo: etcdDruidRepo}
	return source.Transfer(ctx, cv, repo.Repo, "--recursive", "--copy-resources")
}

// referencedVersion returns the version the aggregate references component
// at, or "" when it does not.
func referencedVersion(component string) (string, error) {
	data, err := os.ReadFile(constructor)
	if err != nil {
		return "", err
	}
	var doc struct {
		Components []struct {
			ComponentReferences []struct {
				ComponentName string `yaml:"componentName"`
				Version       string `yaml:"version"`
			} `yaml:"componentReferences"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parsing %s: %w", constructor, err)
	}
	for _, c := range doc.Components {
		for _, ref := range c.ComponentReferences {
			if ref.ComponentName == component {
				return ref.Version, nil
			}
		}
	}
	return "", nil
}
