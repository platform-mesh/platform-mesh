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

// Package releasenotes renders the Markdown release notes for a range of
// commits of this repository.
//
// Generate reads the range from git, the pull requests and the commits of
// components built in other repositories from GitHub, and executes the
// embedded template. The git-backed pieces (Ref, FirstParentLog,
// Components, GroupChanges, ShowFile, ReadChart, OtherComponentRows,
// ThirdPartyVersions) are exported for the integration tests under
// tests/integration/release-notes.
package releasenotes
