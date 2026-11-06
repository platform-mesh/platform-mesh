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

package releaseutils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// OCMRepository is the OCM repository the constructors are built into.
type OCMRepository struct {
	Repo string
	// Signature is the signature name to sign with; empty skips signing.
	Signature string
}

// Add builds a constructor into the repository.
// workingDir resolves the constructor's file inputs, empty keeps ocm's default.
// env is a map of ENV_VAR=VALUE pairs to set in the environment for ocm, primarily for ocm to expand in constructors.
// Returns the component versions ocm reports, built or skipped, as name:version.
func (repo OCMRepository) Add(ctx context.Context, constructor, workingDir string, env map[string]string) ([]string, error) {
	args := []string{
		"add", "component-versions",
		"--repository", repo.Repo,
		"--constructor", constructor,
		"--component-version-conflict-policy", "skip",
		"--skip-reference-digest-processing=false",
		"-o", "json",
	}
	if workingDir != "" {
		args = append(args, "--working-directory", workingDir)
	}
	cmd := exec.CommandContext(ctx, "ocm", args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("building %s: %w", constructor, err)
	}

	var descriptors []struct {
		Component struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"component"`
	}
	if err := json.Unmarshal(out, &descriptors); err != nil {
		return nil, fmt.Errorf("decoding ocm output for %s: %w", constructor, err)
	}
	cvs := make([]string, 0, len(descriptors))
	for _, d := range descriptors {
		cvs = append(cvs, d.Component.Name+":"+d.Component.Version)
	}
	return cvs, nil
}

// Exists reports whether the repository holds the component version.
// This is primarily for transferring between registries, e.g. when
// transferring a signed CV from a CTF to a remote registry to prevent
// check before accidentally overriding an existing tag in the remote
// registry.
// Any failure other than "not found" is returned, so a flaky registry
// cannot read as "absent".
// TODO(ntnn): Would be nice to have a flag in ocm for this.
func (repo OCMRepository) Exists(ctx context.Context, cv string) (bool, error) {
	// An archive that is not there yet holds nothing; ocm reports that as
	// an unreadable repository, not as a missing version.
	if path, ok := strings.CutPrefix(repo.Repo, CTFPrefix); ok {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
	}
	get := exec.CommandContext(ctx, "ocm", "get", "component-version", repo.Repo+"//"+cv, "-o", "json")
	var stderr bytes.Buffer
	get.Stderr = &stderr
	err := get.Run()
	if err == nil {
		return true, nil
	}
	if strings.Contains(stderr.String(), "component version not found") {
		return false, nil
	}
	return false, fmt.Errorf("checking for %s in %s: %w\n%s", cv, repo.Repo, err, stderr.String())
}

// Transfer copies a component version from the repository to target.
// flags are passed to ocm transfer as given, e.g. --recursive.
func (repo OCMRepository) Transfer(ctx context.Context, cv, target string, flags ...string) error {
	args := append([]string{"transfer", "component-version", repo.Repo + "//" + cv, target}, flags...)
	err := Retry(ctx, RegistryAttempts, RegistryRetryDelay, func() error {
		cmd := exec.CommandContext(ctx, "ocm", args...)
		cmd.Stderr = os.Stderr
		return cmd.Run()
	})
	if err != nil {
		return fmt.Errorf("transferring %s to %s: %w", cv, target, err)
	}
	return nil
}

// Sign signs each component version that does not already verify with the signature.
// An empty Signature skips signing.
func (repo OCMRepository) Sign(ctx context.Context, cvs []string) error {
	if repo.Signature == "" {
		return nil
	}
	for _, cv := range cvs {
		ref := repo.Repo + "//" + cv
		verify := exec.CommandContext(ctx, "ocm", "verify", "component-version", "--signature", repo.Signature, ref)
		if err := verify.Run(); err == nil {
			log.Printf("%s already signed", cv)
			continue
		}
		log.Printf("signing %s", cv)
		sign := exec.CommandContext(ctx, "ocm", "sign", "component-version", "--signature", repo.Signature, ref)
		sign.Stderr = os.Stderr
		if err := sign.Run(); err != nil {
			return fmt.Errorf("signing %s: %w", cv, err)
		}
	}
	return nil
}
