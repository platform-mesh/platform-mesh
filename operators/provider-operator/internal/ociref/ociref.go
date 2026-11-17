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

package ociref

import (
	"fmt"
	"strings"

	"go.platform-mesh.io/provider-operator/pkg/ocm"
)

// StripScheme removes the scheme (e.g. "oci://", "http://") from an image reference,
// returning the bare host/path portion.
func StripScheme(ref string) string {
	if i := strings.Index(ref, "://"); i >= 0 {
		return ref[i+3:]
	}
	return ref
}

// SplitRegistry splits an OCM/OCI registry root (e.g. "ghcr.io/platform-mesh") into the
// host (baseURL) and the remaining sub-path for a delivery.ocm.software Repository.
func SplitRegistry(registry string) (baseURL, subPath string) {
	registryWithoutScheme := StripScheme(registry)
	scheme, _ := strings.CutSuffix(registry, registryWithoutScheme)
	baseURL, subPath, _ = strings.Cut(registryWithoutScheme, "/")

	if scheme != "" {
		baseURL = scheme + baseURL
	}
	return
}

// NormalizeOCIURL turns an OCM-resolved imageReference (and version) into a clean
// oci://host/repository URL suitable for a Flux OCIRepository spec.url. Plain-HTTP
// registries store http:// in the imageReference; we normalise to oci:// regardless
// because Flux always uses that scheme (plain-HTTP is enabled via spec.insecure).
func NormalizeOCIURL(imageRef, version string) (string, error) {
	imageRef = StripScheme(imageRef)
	url := strings.TrimSuffix("oci://"+imageRef, ":"+version)
	spec, err := ocm.ParseRef(url)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s/%s", spec.Scheme, spec.Host, spec.Repository), nil
}

// ExtractRepoURL strips the scheme and the final path component (image name) from an
// OCI image reference, returning the parent repository path (host/repo-prefix).
func ExtractRepoURL(imageRef string) (string, error) {
	imageRef = StripScheme(imageRef)
	baseURL := strings.Split(imageRef, ":")[0]
	lastSlash := strings.LastIndex(baseURL, "/")
	if lastSlash == -1 {
		return "", fmt.Errorf("invalid imageReference format: %s", imageRef)
	}
	return baseURL[:lastSlash], nil
}
