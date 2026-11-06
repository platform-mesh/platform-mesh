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

// ocm-build-image publishes the image component of one operator or service directory.
// The CV is built and signed in a temporary CTF, so the signature
// covers the SBOM blobs, and is then transferred to the repo.
//
// Usage: ocm-build-image <component dir> <sbom dir>
//
// <sbom dir> holds <image>/sbom-cyclonedx.json and <image>/sbom-spdx.json for
// every image of the component.
//
// Run from the repository root.
//
// Environment:
//
//	PM_VERSION  required; the component version
//	OCM_REPO    target repository, ctf::<path> builds into an archive instead
//	IMAGE_REPO  OCI registry the images were pushed to, <base>/platform-mesh unless set
//	REPO_URL    git repository recorded as the component's source
//	COMMIT      git commit recorded as the component's source
//	SIGNATURE   sign the component versions with this signature
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	if len(os.Args) != 3 {
		return errors.New("usage: ocm-build-image <component dir> <sbom dir>")
	}
	dir := filepath.Clean(os.Args[1])
	sbomDir := filepath.Clean(os.Args[2])

	constructor := filepath.Join(dir, "component-constructor.yaml")
	if _, err := os.Stat(constructor); err != nil {
		return fmt.Errorf("checking for %s: %w", constructor, err)
	}
	if _, err := os.Stat(sbomDir); err != nil {
		return fmt.Errorf("checking for SBOM directory %s: %w", sbomDir, err)
	}

	version := os.Getenv("PM_VERSION")
	if version == "" {
		return errors.New("PM_VERSION must be set")
	}

	ocmRepo := envOr("OCM_REPO", defaultOCMRepo)
	// An archive target still mirrors the published registry.
	base := ocmRepo
	if strings.HasPrefix(base, ctfPrefix) {
		base = defaultOCMRepo
	}

	commit := os.Getenv("COMMIT")
	if commit == "" {
		var err error
		commit, err = gitHead(ctx)
		if err != nil {
			return err
		}
	}

	env := map[string]string{
		"PM_VERSION": version,
		"IMAGE_REPO": envOr("IMAGE_REPO", base+"/platform-mesh"),
		"REPO_URL":   envOr("REPO_URL", defaultRepoURL),
		"COMMIT":     commit,
	}

	tmp, err := os.MkdirTemp("", "ocm-build-image-")
	if err != nil {
		return fmt.Errorf("creating transport directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	transport := releaseutils.OCMRepository{
		Repo:      ctfPrefix + filepath.Join(tmp, "transport.ctf"),
		Signature: os.Getenv("SIGNATURE"),
	}
	target := releaseutils.OCMRepository{Repo: ocmRepo}

	log.Printf("building %s", constructor)
	cvs, err := transport.Add(ctx, constructor, sbomDir, env)
	if err != nil {
		return err
	}
	for _, cv := range cvs {
		if target.Exists(ctx, cv) {
			return fmt.Errorf("%s already exists in %s; published versions are immutable", cv, ocmRepo)
		}
	}
	if err := transport.Sign(ctx, cvs); err != nil {
		return err
	}
	for _, cv := range cvs {
		log.Printf("publishing %s to %s", cv, ocmRepo)
		if err := transport.Transfer(ctx, cv, ocmRepo); err != nil {
			return err
		}
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
