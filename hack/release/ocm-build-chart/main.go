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

// ocm-build-chart publishes one chart directory.
//
// Usage: ocm-build-chart <charts dir>
//
// Run from the repository root.
//
// Environment:
//
//	PM_VERSION     required; the chart and component version
//	OCM_REPO       target repository, ctf::<path> builds into an archive instead
//	CHART_REPO     OCI registry for the archives, <base>/helm-charts unless set
//	APP_VERSION    defaults to the Chart.yaml appVersion
//	REPO_URL       git repository recorded as the component's source
//	COMMIT         git commit recorded as the component's source
//	SIGNATURE      sign the component versions with this signature
//	SKIP_PUSH      true keeps the archive local
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

const (
	defaultOCMRepo = "ghcr.io/platform-mesh"
	defaultRepoURL = "https://github.com/platform-mesh/platform-mesh"
	ctfPrefix      = "ctf::"
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
	if len(os.Args) != 2 {
		return errors.New("usage: ocm-build-chart <charts dir>")
	}
	dir := filepath.Clean(os.Args[1])
	chart := filepath.Base(dir)

	meta, err := readChart(dir)
	if err != nil {
		return err
	}

	version := os.Getenv("PM_VERSION")
	if version == "" {
		return errors.New("PM_VERSION must be set")
	}

	ocmRepo := envOr("OCM_REPO", defaultOCMRepo)

	// An archive target still mirrors the published registries.
	base := ocmRepo
	if strings.HasPrefix(base, ctfPrefix) {
		base = defaultOCMRepo
	}

	chartRepo := envOr("CHART_REPO", base+"/helm-charts")
	appVersion := appVersion(os.Getenv("APP_VERSION"), meta.AppVersion, version)

	commit := os.Getenv("COMMIT")
	if commit == "" {
		commit, err = gitHead(ctx)
		if err != nil {
			return err
		}
	}

	env := map[string]string{
		"PM_VERSION":  version,
		"CHART_REPO":  chartRepo,
		"APP_VERSION": appVersion,
		"REPO_URL":    envOr("REPO_URL", defaultRepoURL),
		"COMMIT":      commit,
	}
	chartEnv, err := readChartEnv(dir)
	if err != nil {
		return err
	}
	maps.Copy(env, chartEnv)

	log.Printf("%s: version %s appVersion %s CHART_REPO %s", chart, version, appVersion, chartRepo)

	if os.Getenv("SKIP_PUSH") != "true" {
		archive := helmArchive{
			Dir:                 dir,
			Repo:                chartRepo,
			Version:             version,
			AppVersion:          appVersion,
			OverrideAppVersion:  appVersion != meta.AppVersion,
			ResolveDependencies: len(meta.Dependencies) > 0,
		}
		if err := archive.publish(ctx); err != nil {
			return err
		}
	}

	constructor := filepath.Join(dir, "component-constructor.yaml")
	if _, err := os.Stat(constructor); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("checking for %s: %w", constructor, err)
		}
		log.Printf("no component-constructor.yaml in %s, archive only", dir)
		return nil
	}

	repo := releaseutils.OCMRepository{
		Repo:      ocmRepo,
		Signature: os.Getenv("SIGNATURE"),
	}
	log.Printf("building %s into %s", constructor, ocmRepo)
	cvs, err := repo.Add(ctx, constructor, "", env)
	if err != nil {
		return err
	}
	if err := repo.Sign(ctx, cvs); err != nil {
		return err
	}

	log.Printf("done: %s", strings.Join(cvs, " "))
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func gitHead(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("reading git HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
