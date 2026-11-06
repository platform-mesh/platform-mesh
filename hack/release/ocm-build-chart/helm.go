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

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

const (
	pushAttempts = 3
	// GHCR occasionally answers "tag: not found" right after an upload to a
	// freshly created package, the second attempt succeeds.
	pushRetryDelay = 10 * time.Second
)

// helmArchive packages one chart directory and pushes it to an OCI registry.
type helmArchive struct {
	Dir        string
	Repo       string
	Version    string
	AppVersion string
	// OverrideAppVersion passes --app-version, false keeps Chart.yaml's.
	OverrideAppVersion bool
	// ResolveDependencies runs hack/charts/resolve-dependencies.sh when the chart has dependencies but no charts/ directory yet.
	ResolveDependencies bool
}

// publish packages and pushes the archive unless CHART_REPO already has it.
func (archive helmArchive) publish(ctx context.Context) error {
	name := filepath.Base(archive.Dir)
	ref := fmt.Sprintf("oci://%s/%s", archive.Repo, name)

	published, err := archive.published(ctx, ref)
	if err != nil {
		return err
	}
	if published {
		log.Printf("chart %s:%s already published", name, archive.Version)
		return nil
	}

	if err := archive.resolveDependencies(ctx); err != nil {
		return err
	}

	dest, err := os.MkdirTemp("", "ocm-build-chart-")
	if err != nil {
		return fmt.Errorf("creating archive directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dest) }()

	args := []string{"package", archive.Dir, "--version", archive.Version, "--destination", dest}
	if archive.OverrideAppVersion {
		args = append(args, "--app-version", archive.AppVersion)
	}
	pkg := exec.CommandContext(ctx, "helm", args...)
	pkg.Stderr = os.Stderr
	if err := pkg.Run(); err != nil {
		return fmt.Errorf("packaging %s: %w", archive.Dir, err)
	}

	tgz := filepath.Join(dest, fmt.Sprintf("%s-%s.tgz", name, archive.Version))
	err = releaseutils.Retry(ctx, pushAttempts, pushRetryDelay, func() error {
		push := exec.CommandContext(ctx, "helm", "push", tgz, "oci://"+archive.Repo)
		push.Stderr = os.Stderr
		return push.Run()
	})
	if err != nil {
		return fmt.Errorf("pushing %s to %s: %w", filepath.Base(tgz), archive.Repo, err)
	}
	return nil
}

// published reports whether CHART_REPO already holds the archive.
// helm has one exit code for every failure, so only a "not found" on stderr
// reads as absent; anything else is returned, as GHCR accepts re-pushes.
func (archive helmArchive) published(ctx context.Context, ref string) (bool, error) {
	show := exec.CommandContext(ctx, "helm", "show", "chart", ref, "--version", archive.Version)
	var stderr bytes.Buffer
	show.Stderr = &stderr
	err := show.Run()
	if err == nil {
		return true, nil
	}
	if strings.Contains(stderr.String(), "not found") {
		return false, nil
	}
	return false, fmt.Errorf("checking for %s:%s: %w\n%s", ref, archive.Version, err, stderr.String())
}

func (archive helmArchive) resolveDependencies(ctx context.Context) error {
	if !archive.ResolveDependencies {
		return nil
	}
	_, err := os.Stat(filepath.Join(archive.Dir, "charts"))
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking %s/charts: %w", archive.Dir, err)
	}

	resolve := exec.CommandContext(ctx, "hack/charts/resolve-dependencies.sh", filepath.Dir(archive.Dir))
	resolve.Stderr = os.Stderr
	if err := resolve.Run(); err != nil {
		return fmt.Errorf("resolving %s's dependencies: %w", archive.Dir, err)
	}
	return nil
}
