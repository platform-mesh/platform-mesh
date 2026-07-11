# resource-broker

`resource-broker` is a Kubernetes operator to back generic APIs with multiple providers.

## Overview

Running services requires other services - applications requires databases, certificates, etc.pp.

These services are implemented by many different providers but with common characteristics.

`resource-broker` allows platforms and teams to generalize vendor APIs to a common, transferable spec and to route resources to providers based on policies and capabilities.

## Architecture

The system is built around three roles:

* **Platform / Coordination Cluster**: Runs the `resource-broker` operator. Also used to record e.g. migrations and migration states.
* **Consumer Clusters**: where users create high-level resources (e.g., `Certificate`, `VirtualMachine`).
* **Provider Clusters**: where the actual realization of the resource happens (e.g., `cert-manager` issuing a cert, or a VM controller provisioning a VM).

### Core Concepts

#### AcceptAPI

Providers define `AcceptAPI` resources to declare which APIs they can serve and under which conditions.

```yaml
apiVersion: broker.platform-mesh.io/v1alpha1
kind: AcceptAPI
metadata:
  name: internal-certs
spec:
  gvr:
    group: example.platform-mesh.io
    version: v1alpha1
    resource: certificates
  filters:
    - key: fqdn
      suffix: internal.corp
```

In this example, the provider claims it can handle `Certificate` resources, but *only* if the `fqdn` field finishes with `internal.corp`.

#### Lifecycle

When a provider can no longer back a resource (either due to changes on the resource or due to changes in the providers `AcceptAPI`) `resource-broker` first chooses a new provider and instantiates the resource before switching the consumer over and deleting the resource in the previous provider.

This is to reduce the downtime for the consumer as much as possible, as some services - like databases - can take a while to startup.

#### Migration

`resource-broker` supports migrating resources between providers. For example, moving a workload from one provider to another based on policy changes or capacity needs. This is managed via `Migration` and `MigrationConfiguration` resources, allowing for defined stages (e.g., Initial Data Copy -> Cutover -> Finalize).

### Running Examples

The `examples/` directory contains comprehensive walkthroughs. The best place to start is the **Certificate Brokering with kcp** example:

* [Brokering Certificates with kcp](./examples/broker-certificates/README.md)

## License

This project is licensed under [Apache-2.0](../../LICENSE).

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
