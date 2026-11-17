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

package util

import "strings"

// nameEncoder is a strings.Replacer that percent-encodes the characters that
// OpenFGA forbids in the identifier portion of an object key.
var nameEncoder = strings.NewReplacer(
	"%", "%25",
	":", "%3A",
)

// EncodeName percent-encodes the characters that OpenFGA forbids in the
// identifier portion of an object key.
//
// OpenFGA requires an object to contain exactly one colon, the separator
// between type and identifier, so a Kubernetes resource name that itself
// contains a colon (e.g. the ClusterRole "system:controller:foo") produces an
// object that the server rejects outright.
//
// Percent-encoding is used rather than a plain substitution because it is
// injective: "system:controller:foo" and "system.controller.foo" are distinct,
// both-legal resource names and must not map onto the same key. '%' is encoded
// as well so the transformation stays injective for callers outside Kubernetes;
// a Kubernetes name can never contain '%' itself.
//
// Names that contain neither character are returned unchanged, so keys that
// OpenFGA already accepts today keep their current form.
func EncodeName(name string) string {
	return nameEncoder.Replace(name)
}
