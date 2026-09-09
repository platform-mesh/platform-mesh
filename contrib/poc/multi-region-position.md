# Multi-Region Status Quo and Position

This paper explains the current status of multi-region support in platform-mesh. It does not touch on multi-zone support (see differentiation below).

## Differentiation: Region vs Zone

Regions are characterized by a significant geographical distance between them, resulting in round-trip times between 20-250ms. For example cross-continent, transatlantic or transpacific communication.

Zones are geographically redundant locations with round-trip times between >1-20ms. For example locations on the same continent, with private backbones.

## Status Quo - Global Control-Plane

In order to run PlatformMesh across multiple regions acting as a global control-plane, it is split on the kcp level using shards. A kcp shard connects to its own etcd cluster and a centrally run CacheServer. Additionally shards communicate with each other during scheduling of logicalclusters. A shard can have multiple replicas, with one being active. Shards can be deployed as part of Kubernetes clusters or on standalone infrastructure. Shards can be deployed as part of Kubernetes clusters or on standalone infrastructure.

Lastly a region can have multiple shards to extend its capacity horizontally.

Here is a simplified example of a two-region setup, with the region `Europe` acting as the global control plane.

```mermaid
flowchart TB
    subgraph America
        subgraph USUserGroup["user"]
            USUser(["us-user / controller"])
        end
        subgraph USPMesh["platform-mesh"]
            subgraph US1Shard["kcp shard america1"]
                US1Active["kcp replica (active)"]
                US1Passive1["kcp replica (passive)"]
                US1Passive2["kcp replica (passive)"]
                US1VW["virtual-workspace"]
            end
            subgraph US2Shard["kcp shard america2"]
                US2Active["kcp replica (active)"]
                US2Passive1["kcp replica (passive)"]
                US2Passive2["kcp replica (passive)"]
                US2VW["virtual-workspace"]
            end
            subgraph US1Etcd["etcd cluster america1"]
                US1Etcd1["etcd"]
                US1Etcd2["etcd"]
                US1Etcd3["etcd"]
            end
            subgraph US2Etcd["etcd cluster america2"]
                US2Etcd1["etcd"]
                US2Etcd2["etcd"]
                US2Etcd3["etcd"]
            end
            US1Shard --> US1Etcd
            US2Shard --> US2Etcd
            US1Shard <-->|logicalcluster scheduling| US2Shard
            USFP["frontproxy"]
        end
    end

    subgraph Europe
        subgraph EUUserGroup["user"]
            EUUser(["eu-user / controller"])
        end
        subgraph EUPMesh["platform-mesh"]
            subgraph EU1Shard["kcp shard europe1"]
                EU1Active["kcp replica (active)"]
                EU1Passive1["kcp replica (passive)"]
                EU1Passive2["kcp replica (passive)"]
                EU1VW["virtual-workspace"]
            end
            subgraph EU2Shard["kcp rootshard"]
                EU2Active["kcp replica (active)"]
                EU2Passive1["kcp replica (passive)"]
                EU2Passive2["kcp replica (passive)"]
                EU2VW["virtual-workspace"]
            end
            subgraph EU1Etcd["etcd cluster europe1"]
                EU1Etcd1["etcd"]
                EU1Etcd2["etcd"]
                EU1Etcd3["etcd"]
            end
            subgraph EU2Etcd["etcd cluster europe2"]
                EU2Etcd1["etcd"]
                EU2Etcd2["etcd"]
                EU2Etcd3["etcd"]
            end
            CacheServer["CacheServer"]
            EUPortal["PlatformMesh portal"]
            OpenFGA["OpenFGA"]
            EUFP["frontproxy"]

            EU1Shard --> EU1Etcd
            EU2Shard --> EU2Etcd
            EU1Shard <-->|logicalcluster scheduling| EU2Shard
            EUFP -->|discovery / auth| EU2Shard
        end
    end

    EU1Shard <--> CacheServer
    EU2Shard <--> CacheServer
    US1Shard <--> CacheServer
    US2Shard <--> CacheServer
    EU1Shard -->|authz check| OpenFGA
    EU2Shard -->|authz check| OpenFGA
    US1Shard -->|authz check| OpenFGA
    US2Shard -->|authz check| OpenFGA
    EUFP -->|auth / data-plane proxying| EU1Shard
    EUFP -->|auth / data-plane proxying| EU2Shard
    EUFP -->|auth / data-plane proxying| US1Shard
    EUFP -->|auth / data-plane proxying| US2Shard
    USFP -->|discovery / auth| EU2Shard
    USFP -->|auth / data-plane proxying| EU1Shard
    USFP -->|auth / data-plane proxying| EU2Shard
    USFP -->|auth / data-plane proxying| US1Shard
    USFP -->|auth / data-plane proxying| US2Shard
    EUUser --> EUFP
    EUUser --> EU1VW
    EUUser --> EU2VW
    EUUser --> US1VW
    EUUser --> US2VW
    EUUser --> EUPortal
    USUser --> USFP
    USUser --> EU1VW
    USUser --> EU2VW
    USUser --> US1VW
    USUser --> US2VW
    USUser --> EUPortal
    EU1Shard <-->|logicalcluster scheduling| US1Shard
    EU1Shard <-->|logicalcluster scheduling| US2Shard
    EU2Shard <-->|logicalcluster scheduling| US1Shard
    EU2Shard <-->|logicalcluster scheduling| US2Shard
```

## Resulting Next Steps

The following improvements to the current architecture should be made in the mid-term to improve its viability for multi-region to minimize and streamline cross-region connections in a global-control plane model.

* Build a distributed CacheServer
* Run distributed OpenFGA or allow for Kubernetes RBAC to be configurable in platform-mesh
* Make shards not talk to each other for scheduling logicalclusters
* Run portal distributed across regions, with a shared database or create per region portals

## Far Horizon

Multi-region setups need to minimize the amount of traffic and requests made between regions. In the long-term the global control-plane should take in more of an indexing role, providing clients with information how to connect to all regional control planes, similar to the Cloud Hyperscaler model.
