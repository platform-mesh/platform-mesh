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
	"os"
	"strings"
)

const (
	// DefaultOCMRepo is the repository the components are published to.
	DefaultOCMRepo = "ghcr.io/platform-mesh"
	// DefaultRepoURL is the git repository recorded as the components' source.
	DefaultRepoURL = "https://github.com/platform-mesh/platform-mesh"
	// CTFPrefix marks an OCM repository reference that is a local archive.
	CTFPrefix = "ctf::"
)

// EnvOr returns the environment variable, or fallback when it is unset or empty.
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// OCMRepo returns OCM_REPO, or DefaultOCMRepo when unset.
func OCMRepo() string {
	return EnvOr("OCM_REPO", DefaultOCMRepo)
}

// BaseRegistry returns the registry whose artifacts the constructors built into ocmRepo reference.
// An archive target still mirrors the published registry.
func BaseRegistry(ocmRepo string) string {
	if strings.HasPrefix(ocmRepo, CTFPrefix) {
		return DefaultOCMRepo
	}
	return ocmRepo
}
