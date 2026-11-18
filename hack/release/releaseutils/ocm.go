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
	"io"
	"log"
	"os"
	"os/exec"
	"slices"
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
//
// A failed build is retried, as registries fail transiently. A version that
// already exists fails the first attempt for good, published versions are
// immutable. Later attempts skip existing versions instead, as a failed
// attempt may have published some of the constructor's components already.
func (repo OCMRepository) Add(ctx context.Context, constructor, workingDir string, env map[string]string) ([]string, error) {
	args := []string{
		"add", "component-versions",
		"--repository", repo.Repo,
		"--constructor", constructor,
		"--skip-reference-digest-processing=false",
		"-o", "json",
	}
	if workingDir != "" {
		args = append(args, "--working-directory", workingDir)
	}

	out, err := ocmCall{
		Args: func(attempt int) []string {
			if attempt == 1 {
				return args
			}
			return append(slices.Clone(args), "--component-version-conflict-policy", "skip")
		},
		Env: env,
		Permanent: func(attempt int, stderr string) bool {
			return attempt == 1 && strings.Contains(stderr, "already exists in target repository")
		},
	}.run(ctx)
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
// Any failure other than "not found" is retried and then returned, so a
// flaky registry cannot read as "absent".
// TODO(ntnn): Would be nice to have a flag in ocm for this.
func (repo OCMRepository) Exists(ctx context.Context, cv string) (bool, error) {
	// An archive that is not there yet holds nothing; ocm reports that as
	// an unreadable repository, not as a missing version.
	if path, ok := strings.CutPrefix(repo.Repo, CTFPrefix); ok {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
	}
	notFound := func(stderr string) bool {
		return strings.Contains(stderr, "component version not found")
	}
	_, err := ocmCall{
		Args:      ocmArgs("get", "component-version", repo.Repo+"//"+cv, "-o", "json"),
		Quiet:     true,
		Permanent: func(_ int, stderr string) bool { return notFound(stderr) },
	}.run(ctx)
	if err == nil {
		return true, nil
	}
	var failed *ocmError
	if errors.As(err, &failed) && notFound(failed.stderr) {
		return false, nil
	}
	return false, fmt.Errorf("checking for %s in %s: %w", cv, repo.Repo, err)
}

// Transfer copies a component version from the repository to target.
// flags are passed to ocm transfer as given, e.g. --recursive.
func (repo OCMRepository) Transfer(ctx context.Context, cv, target string, flags ...string) error {
	args := append([]string{"transfer", "component-version", repo.Repo + "//" + cv, target}, flags...)
	if _, err := (ocmCall{Args: ocmArgs(args...)}).run(ctx); err != nil {
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
		// One attempt: a version that is not signed yet fails this as well,
		// and signing a version twice does no harm.
		_, err := ocmCall{
			Args:      ocmArgs("verify", "component-version", "--signature", repo.Signature, ref),
			Quiet:     true,
			Permanent: func(int, string) bool { return true },
		}.run(ctx)
		if err == nil {
			log.Printf("%s already signed", cv)
			continue
		}

		log.Printf("signing %s", cv)
		_, err = ocmCall{
			Args: ocmArgs("sign", "component-version", "--signature", repo.Signature, ref),
		}.run(ctx)
		if err != nil {
			return fmt.Errorf("signing %s: %w", cv, err)
		}
	}
	return nil
}

// ocmBinary is the ocm CLI and ocmRetryDelay the pause between the attempts
// of an ocmCall; variables for the tests to replace.
var (
	ocmBinary     = "ocm"
	ocmRetryDelay = RegistryRetryDelay
)

// ocmCall is an invocation of the ocm CLI, retried when it fails.
type ocmCall struct {
	// Args returns the arguments of an attempt, counted from 1.
	Args func(attempt int) []string
	// Env is added to ocm's environment as NAME=VALUE.
	Env map[string]string
	// Quiet keeps ocm's stderr off the terminal; a returned error carries it instead.
	Quiet bool
	// Permanent reports whether a failed attempt is not worth retrying, given
	// what it wrote to stderr. Unset retries every failure.
	Permanent func(attempt int, stderr string) bool
}

// ocmArgs returns the same arguments for every attempt.
func ocmArgs(args ...string) func(int) []string {
	return func(int) []string { return args }
}

// ocmError is a failed attempt of an ocmCall.
type ocmError struct {
	err    error
	stderr string
	// quiet says stderr was not printed, so Error includes it.
	quiet bool
}

func (e *ocmError) Error() string {
	if e.quiet && e.stderr != "" {
		return e.err.Error() + "\n" + strings.TrimSpace(e.stderr)
	}
	return e.err.Error()
}

func (e *ocmError) Unwrap() error { return e.err }

// run runs ocm up to RegistryAttempts times and returns the stdout of the
// attempt that succeeded. The error of a failed call is an *ocmError.
func (call ocmCall) run(ctx context.Context) ([]byte, error) {
	var out []byte
	attempt := 0
	err := Retry(ctx, RegistryAttempts, ocmRetryDelay, func() error {
		attempt++
		cmd := exec.CommandContext(ctx, ocmBinary, call.Args(attempt)...)
		cmd.Env = os.Environ()
		for k, v := range call.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if !call.Quiet {
			cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
		}
		var err error
		out, err = cmd.Output()
		if err == nil {
			return nil
		}
		failed := &ocmError{err: err, stderr: stderr.String(), quiet: call.Quiet}
		if call.Permanent != nil && call.Permanent(attempt, failed.stderr) {
			return Permanent(failed)
		}
		return failed
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
