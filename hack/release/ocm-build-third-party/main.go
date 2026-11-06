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

// ocm-build-third-party publishes the component versions of one third_party
// directory's constructor to OCM_REPO.
//
// Usage: ocm-build-third-party <third_party dir>
//
// Run from the repository root.
//
// Environment:
//
//	PM_VERSION  required; the component version
//	OCM_REPO    target repository, ctf::<path> builds into an archive instead
//	SIGNATURE   sign the component versions with this signature
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

const defaultOCMRepo = "ghcr.io/platform-mesh"

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
		return errors.New("usage: ocm-build-third-party <third_party dir>")
	}
	dir := filepath.Clean(os.Args[1])

	constructor := filepath.Join(dir, "component-constructor.yaml")
	if _, err := os.Stat(constructor); err != nil {
		return fmt.Errorf("checking for %s: %w", constructor, err)
	}

	version := os.Getenv("PM_VERSION")
	if version == "" {
		return errors.New("PM_VERSION must be set")
	}
	ocmRepo := os.Getenv("OCM_REPO")
	if ocmRepo == "" {
		ocmRepo = defaultOCMRepo
	}

	repo := releaseutils.OCMRepository{
		Repo:      ocmRepo,
		Signature: os.Getenv("SIGNATURE"),
	}
	log.Printf("building %s into %s", constructor, ocmRepo)
	cvs, err := repo.Add(ctx, constructor, "", map[string]string{"PM_VERSION": version})
	if err != nil {
		return err
	}
	if err := repo.Sign(ctx, cvs); err != nil {
		return err
	}

	log.Printf("done: %s", strings.Join(cvs, " "))
	return nil
}
