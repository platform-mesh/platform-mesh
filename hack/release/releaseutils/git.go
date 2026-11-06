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
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Commit returns COMMIT, or the commit of git HEAD when unset.
func Commit(ctx context.Context) (string, error) {
	if commit := os.Getenv("COMMIT"); commit != "" {
		return commit, nil
	}
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("reading git HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
