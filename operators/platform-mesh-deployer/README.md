# platform-mesh-deployer

Operator that deploys and manages Platform Mesh installations on a management
cluster. It reconciles the `deploy.platform-mesh.io/v1alpha1` resources
(`PlatformMesh`, `Module`, `ModuleSetup`) defined in the shared
[`apis`](../../apis) module.

## Development

```bash
task build      # go build ./...
task lint       # format + golangci-lint
task test       # unit tests
task generate   # regenerate CRDs (config/crd) and deepcopy code
```

The CRDs are installed on the management cluster (not in kcp), so no `apigen`
step is required.

### Running it

#### Tilt setup

The deployer is not part of the repository's Tilt environment
([contrib/tilt/README.md](../../contrib/tilt/README.md)), which deploys
Platform Mesh from the in-repo charts with kcp-operator.

It runs kcp-operator's config and workload controller groups inside its own
manager, from the ntnn/kcp-operator fork pinned by the `replace` in `go.mod`.
Nothing deploys kcp-operator itself; only its CRDs are installed, from
`config/bases/kcp-operator/crds`, pinned to the same commit — bump the two
together.

#### Kind setup

A single-cluster environment stood up with plain `kubectl`, `helm` and
`kind`. Step by step, including the pieces that are
easy to miss (the CoreDNS sslip.io block, the gateway NodePort, the kubeconfig
Secret that engages the cluster with itself, and an admin kubeconfig at the end):
[`docs/local-kind.md`](docs/local-kind.md).


## Samples

See [`config/samples`](config/samples) for example resources.
