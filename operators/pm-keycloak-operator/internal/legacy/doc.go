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

// Package legacy serves the deprecated core.platform-mesh.io
// IdentityProviderConfiguration and Invite kinds with the subroutines
// written for keycloak.platform-mesh.io.
//
// A legacy object is converted to its keycloak.platform-mesh.io
// counterpart before a subroutine runs and the result is copied back,
// so the lifecycle patches the object kcp actually holds.
// Removed together with the LegacyAPIs feature gate once installations
// are migrated.
package legacy
