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

package features

import "embed"

// Resources holds the feature-toggle manifests: each feature-<name> subdirectory is the
// manifest tree applied to kcp when the PlatformMesh enables that toggle,
// laid out like manifests/kcp (see subroutines.ApplyDirStructure).
//
//go:embed */*/*.yaml
var Resources embed.FS
