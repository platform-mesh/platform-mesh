# provider-operator

Onboards providers into Platform Mesh: creates their kcp workspace, a kubeconfig scoped to it, and
deploys their runtime components.

## Description

The provider-operator reconciles two resources from `providers.platform-mesh.io`
(`go.platform-mesh.io/apis/providers/v1alpha1`):

- **`ManagedProvider`** (namespaced, on the runtime cluster): a provider that Platform Mesh
  installs and runs. Its reconciler creates the matching `Provider` in kcp, waits until that
  `Provider` is ready (ServiceAccount, RBAC and kubeconfig Secret exist), copies the kubeconfig
  Secret to the runtime cluster and deploys the components listed in
  `spec.runtimeDeployments` as Flux `OCIRepository`/`HelmRepository` and `HelmRelease` objects.
- **`Provider`** (cluster-scoped, in kcp, watched through the providers APIExport endpoint
  slice): its reconciler creates the provider's workspace under `root:providers` and a
  kubeconfig scoped to that workspace.

**Status:**

- The `ManagedProvider` controller and its four subroutines are copied from
  [platform-mesh-operator](../platform-mesh-operator) and registered in the binary. The copy
  leaves out platform-mesh-operator's first step, which waits for the `PlatformMesh` resource.
- The `Provider` controller still exists only in platform-mesh-operator. The `ManagedProvider`
  controller needs it running there: it waits for the `Provider` to become ready and copies the
  kubeconfig Secret that controller writes.
- Nothing deploys provider-operator yet: it has no image and no Helm chart, and
  platform-mesh-operator runs both controllers in production. Do not run provider-operator
  against a cluster where platform-mesh-operator runs; both would reconcile every
  `ManagedProvider`.
- Until platform-mesh-operator's copy is removed, the same code lives in both operators. Fix
  bugs in both.

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
task generate       # config/rbac/role.yaml from the RBAC markers, and the test mocks
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

The `operator` subcommand also takes the `ManagedProvider` flags. Names and defaults are the
same as in platform-mesh-operator:

| Flag | Default | Purpose |
|---|---|---|
| `--kcp-url` | empty: `https://<front-proxy-name>-front-proxy.<namespace>:<port>` | kcp front-proxy URL |
| `--kcp-namespace` | `platform-mesh-system` | namespace of the kcp installation and its admin certificate Secret |
| `--kcp-front-proxy-name` | `frontproxy` | used to build the default URL |
| `--kcp-front-proxy-port` | `8443` | used to build the default URL |
| `--kcp-cluster-admin-secret-name` | `kcp-cluster-admin-client-cert` | Secret with the kcp admin credentials |
| `--subroutines-managed-provider-resource-enabled` | `true` | create the `Provider` in kcp |
| `--subroutines-managed-provider-wait-enabled` | `true` | wait for the `Provider` to become ready |
| `--subroutines-managed-provider-kubeconfig-enabled` | `true` | copy the kubeconfig Secret to the runtime cluster |
| `--subroutines-managed-provider-deploy-enabled` | `true` | deploy `spec.runtimeDeployments` through Flux |

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
