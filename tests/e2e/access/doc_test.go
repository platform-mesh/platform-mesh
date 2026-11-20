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

// Package access exercises the RFC 016 AccessGrant handshake end-to-end
// against a deployed Platform Mesh. The runtime cluster where PM runs
// plays the "remote-side" cluster: the kind profile deploys
// sdk-access-operator and sdk-access-server into platform-mesh-system so
// an AccessIdentity minted there mints a token the PM-side AccessGrant
// uses to call back over an in-cluster Service.
package access
