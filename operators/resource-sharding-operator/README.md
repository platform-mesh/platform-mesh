> [!WARNING]
> This repository is under development and not ready for productive use. It is in an alpha stage — APIs and concepts may change on short notice, including breaking changes or complete removal.

# resource-sharding-operator

## Description

The resource-sharding-operator automatically assigns Kubernetes resources to named shards by applying a label. Downstream operator replicas then filter their watches by shard label, so each replica only caches and reconciles its assigned subset of resources.

Assignment uses a two-path strategy: a mutating webhook handles new resources at admission time (immediate), and a dynamic controller catches anything the webhook missed (fallback). The combination means the system degrades gracefully — the webhook is optional and never blocks resource creation.

## Features

- `ResourceSharding` CRD to declaratively configure sharding for any GVK
- Mutating admission webhook for instant shard assignment at CREATE time (`failurePolicy: Ignore`)
- Dynamic controller as a permanent fallback — watches only unlabeled resources via negative label selector
- Periodic rebalancer that evenly redistributes labels across shards when drift is detected
- Orphan cleanup — strips invalid shard labels from resources belonging to removed shards
- Prometheus metrics for per-shard distribution, imbalance ratio, and assignment counts
- Self-healing — label removal triggers reassignment without manual intervention
- KCP / multicluster-runtime support for multi-workspace deployments

## Getting Started

- For building and running the operator, see [CONTRIBUTING.md](CONTRIBUTING.md).
- To deploy with Helm, see the [helm-charts](https://github.com/platform-mesh/helm-charts) repository.
- For architecture and design rationale, see [docs/architecture.md](docs/architecture.md).
- For usage examples and downstream operator integration, see [docs/usage.md](docs/usage.md).
- For the full design concept and prior-art comparison, see [docs/concept.md](docs/concept.md).

## Quick Example

```yaml
apiVersion: sharding.platform-mesh.io/v1alpha1
kind: ResourceSharding
metadata:
  name: myresource-sharding
spec:
  target:
    group: example.io
    version: v1
    resource: myresources
  shards:
    - name: shard-a
    - name: shard-b
    - name: shard-c
  rebalance:
    interval: 5m
    threshold: 20
```

After creating this resource, the operator labels every `myresource.example.io` object with
`sharding.platform-mesh.io/shard: shard-{a,b,c}`. Downstream operator replicas each set
`DefaultLabelSelector` to their assigned shard name and receive only their subset.

## Requirements

- Go (see [go.mod](go.mod) for the required version)
- Kubernetes 1.29+
- `task` CLI for build automation ([Taskfile.dev](https://taskfile.dev))

## Releasing

Releases are performed automatically through a GitHub Actions workflow.

## Security / Disclosure

If you find a bug that may be a security problem, please follow our instructions in the [security policy](https://github.com/platform-mesh/platform-mesh/security/policy) on how to report it. Do not create GitHub issues for security-related concerns.

## Contributing

Please refer to [CONTRIBUTING.md](CONTRIBUTING.md) for instructions on how to contribute.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md).

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
