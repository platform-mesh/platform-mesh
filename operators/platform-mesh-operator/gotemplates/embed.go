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

package gotemplates

import "embed"

// Resources holds the Go templates the deployment subroutine renders:
// infra/infra and components/infra for the infra cluster, infra/runtime and
// components/runtime for the runtime cluster.
//
//go:embed infra components
var Resources embed.FS
