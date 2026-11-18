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

// Package scratchrepo creates throwaway git repositories for tests and
// examples of the version tooling.
package scratchrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// isolation keeps the developer's git config, signing and hooks and the
// GitHub Actions variables out of the repository.
var isolation = map[string]string{
	"GIT_CONFIG_GLOBAL":   "/dev/null",
	"GIT_CONFIG_NOSYSTEM": "1",
	"GITHUB_REF_NAME":     "",
	"GITHUB_RUN_NUMBER":   "",
}

// Repo is a git repository on branch main that is the working directory
// until Close.
type Repo struct {
	Dir string

	previousDir string
	previousEnv map[string]*string
}

// New creates an empty repository in a temporary directory and makes it
// the working directory.
func New(ctx context.Context) (*Repo, error) {
	previousDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("reading working directory: %w", err)
	}
	dir, err := os.MkdirTemp("", "scratchrepo")
	if err != nil {
		return nil, fmt.Errorf("creating temporary directory: %w", err)
	}
	repo := &Repo{
		Dir:         dir,
		previousDir: previousDir,
		previousEnv: map[string]*string{},
	}
	if err := repo.enter(); err != nil {
		_ = repo.Close()
		return nil, err
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "scratchrepo"},
		{"config", "user.email", "scratchrepo@example.com"},
	} {
		if _, err := repo.Git(ctx, args...); err != nil {
			_ = repo.Close()
			return nil, err
		}
	}
	return repo, nil
}

// Close restores the working directory and environment and deletes the
// repository.
func (repo *Repo) Close() error {
	var errs []error
	if err := os.Chdir(repo.previousDir); err != nil {
		errs = append(errs, fmt.Errorf("restoring working directory: %w", err))
	}
	for key, value := range repo.previousEnv {
		var err error
		if value == nil {
			err = os.Unsetenv(key)
		} else {
			err = os.Setenv(key, *value)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("restoring %s: %w", key, err))
		}
	}
	if err := os.RemoveAll(repo.Dir); err != nil {
		errs = append(errs, fmt.Errorf("removing %s: %w", repo.Dir, err))
	}
	return errors.Join(errs...)
}

// Git runs git in the repository and returns its trimmed output.
func (repo *Repo) Git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repo.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Commit adds an empty commit.
func (repo *Repo) Commit(ctx context.Context, message string) error {
	_, err := repo.Git(ctx, "commit", "-q", "--allow-empty", "-m", message)
	return err
}

// Tag tags HEAD.
func (repo *Repo) Tag(ctx context.Context, name string) error {
	_, err := repo.Git(ctx, "tag", name)
	return err
}

// Head returns the full hash of HEAD.
func (repo *Repo) Head(ctx context.Context) (string, error) {
	return repo.Git(ctx, "rev-parse", "HEAD")
}

func (repo *Repo) enter() error {
	if err := os.Chdir(repo.Dir); err != nil {
		return fmt.Errorf("entering %s: %w", repo.Dir, err)
	}
	for key, value := range isolation {
		if previous, ok := os.LookupEnv(key); ok {
			repo.previousEnv[key] = &previous
		} else {
			repo.previousEnv[key] = nil
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("setting %s: %w", key, err)
		}
	}
	return nil
}
