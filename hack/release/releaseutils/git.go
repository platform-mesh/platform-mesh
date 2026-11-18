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
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gitDirKey is the context key of the directory git runs in.
type gitDirKey struct{}

// WithGitDir returns a context that makes Git, and everything built on it,
// run in dir instead of the working directory: for a caller that is not
// started from the checkout it asks about.
func WithGitDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, gitDirKey{}, dir)
}

// Git runs git in the working directory, or the one of WithGitDir, and
// returns its stdout. A failure carries git's stderr.
func Git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir, ok := ctx.Value(gitDirKey{}).(string); ok {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// Commit returns COMMIT, or the commit of git HEAD when unset.
func Commit(ctx context.Context) (string, error) {
	if commit := os.Getenv("COMMIT"); commit != "" {
		return commit, nil
	}
	out, err := Git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("reading git HEAD: %w", err)
	}
	return strings.TrimSpace(out), nil
}
