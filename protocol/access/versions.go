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

package access

// Handshake protocol versions. Baked into the URL path (e.g.
// /v1/access) and advertised by the /versions probe. Per-version
// message types live in the corresponding vN subpackage.
const (
	V1 = "v1"
)

// VersionsPath is the unauthenticated endpoint served alongside the
// versioned /access endpoints. It returns which handshake versions
// this user-side SDK speaks so PM operators can pick the highest
// mutually-supported version.
const VersionsPath = "/versions"

// AccessPath builds the handshake path for a given version, e.g.
// AccessPath(V1) returns "/v1/access".
func AccessPath(version string) string {
	return "/" + version + "/access"
}

// SupportedVersions is the set of handshake versions this build of the
// protocol library understands, newest first. Both the SDK server and
// the PM-side operator import this list: the server mounts a handler
// per entry, the operator picks the highest entry the server also
// lists via the /versions probe.
var SupportedVersions = []string{V1}

// VersionsResponse is the JSON shape returned by VersionsPath.
type VersionsResponse struct {
	// Versions is the set of handshake protocol versions this
	// endpoint accepts, in server preference order (newest first).
	Versions []string `json:"versions"`
}
