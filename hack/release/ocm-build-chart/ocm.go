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
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
)

// ocmRepository is the OCM repository the constructors are built into.
type ocmRepository struct {
	Repo string
	// Signature is the signature name to sign with; empty skips signing.
	Signature string
}

// add builds a constructor into the repository.
// Returns the component versions ocm reports, built or skipped, as name:version.
func (repo ocmRepository) add(ctx context.Context, constructor, workingDir string, env map[string]string) ([]string, error) {
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

func (repo ocmRepository) sign(ctx context.Context, cvs []string) error {
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
