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

package kcp

import "embed"

// Resources holds the kcp manifests the operator applies during setup: the files in this
// directory target the root workspace, each NN-<name> subdirectory targets the
// workspace <name> and is applied in NN order (see subroutines.ApplyDirStructure).
//
//go:embed *.yaml */*.yaml
var Resources embed.FS
