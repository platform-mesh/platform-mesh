# provider-operator

Onboards providers into Platform Mesh: creates their kcp workspace, a kubeconfig scoped to it, and
deploys their runtime components.

## Description

The provider-operator reconciles two resources from `providers.platform-mesh.io`
(`go.platform-mesh.io/apis/providers/v1alpha1`):

- **`ManagedProvider`** (namespaced, on the runtime cluster): a provider that Platform Mesh
  installs and runs. Its reconciler waits for the `PlatformMesh` named in `spec.platformMeshRef`
  to be ready, creates the matching `Provider` in kcp, waits until that `Provider` is ready
  (ServiceAccount, RBAC and kubeconfig Secret exist), copies the kubeconfig Secret to the
  runtime cluster and deploys the components listed in
  `spec.runtimeDeployments` as Flux `OCIRepository`/`HelmRepository` and `HelmRelease` objects.
- **`Provider`** (cluster-scoped, in kcp, watched through the providers APIExport endpoint
  slice): its reconciler creates the provider's workspace under `root:providers` and a
  kubeconfig scoped to that workspace.

**Status:** scaffold. Both controllers still run in
[platform-mesh-operator](../platform-mesh-operator) and move here next.

## Getting Started

### Prerequisites

- Go, as pinned in the repository-root `go.work`
- [Task](https://taskfile.dev)
- docker, for the image

### Build and test

From `operators/provider-operator`:

```sh
task build
task test
task lint
task docker-build   # IMG defaults to localhost/provider-operator:dev
```

From the repository root, `task verify-provider-operator` runs the checks CI runs.

### Run

```sh
go run . operator --help
```

The binary has one subcommand, `operator`, which starts the controller manager. Common flags
(`--leader-elect`, `--health-probe-bind-address`, `--metrics-bind-address`, logging, tracing)
come from `go.platform-mesh.io/golang-commons/config`.

Production deploys through the `provider-operator` Helm chart in
[platform-mesh/helm-charts](https://github.com/platform-mesh/helm-charts) (not created yet).
`config/` holds kustomize manifests for local use.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Coding agents start with [AGENTS.md](AGENTS.md).

## License

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
