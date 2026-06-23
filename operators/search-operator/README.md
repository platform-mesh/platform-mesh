# search-operator

```mermaid
flowchart TB
    subgraph kcp["kcp Cluster"]
        subgraph PMSys["platform-mesh-system workspace"]
            APIExport["APIExport<br/>core.platform-mesh.io"]
        end

        subgraph OrgWS["Organization Workspace"]
            OrgBinding["APIBinding<br/>→ core.platform-mesh.io"]
            OrgAccountInfo["AccountInfo<br/>name: acme-org<br/>type: organization"]
        end

        subgraph AccWS["Account Workspace"]
            AccBinding["APIBinding<br/>→ core.platform-mesh.io"]
            AccAccountInfo["AccountInfo<br/>name: dev-team<br/>type: account<br/>org: acme-org"]
        end
    end

    subgraph Operator["search-operator"]
        Provider["multicluster-provider<br/>(watches APIExport)"]
        APIBindingCtrl["APIBindingReconciler"]
        WatcherSub["APIBindingWatcherSubroutine<br/>(logs events)"]
        IndexingSub["WorkspaceIndexingSubroutine<br/>(indexes to OpenSearch)"]
    end

    subgraph OS["OpenSearch"]
        Index["platform-mesh-workspaces<br/>index"]
        Doc1["Doc: workspace-org-123<br/>name: acme-org<br/>type: organization<br/>permissions: [...]"]
        Doc2["Doc: workspace-acc-456<br/>name: dev-team<br/>type: account<br/>org: acme-org<br/>permissions: [...]"]
    end

    APIExport -.->|exposes APIs| OrgWS
    APIExport -.->|exposes APIs| AccWS

    Provider -->|watches| APIExport
    Provider -->|receives events from| OrgBinding
    Provider -->|receives events from| AccBinding

    APIBindingCtrl --> WatcherSub
    APIBindingCtrl --> IndexingSub

    IndexingSub -->|reads| OrgAccountInfo
    IndexingSub -->|reads| AccAccountInfo
    IndexingSub -->|writes| Index

    Index --> Doc1
    Index --> Doc2
```

```mermaid
sequenceDiagram
    participant User
    participant Platform as Platform Mesh
    participant kcp as kcp
    participant AccOp as account-operator
    participant SearchOp as search-operator
    participant OS as OpenSearch

    User->>Platform: Create new account "dev-team"
    Platform->>kcp: Create workspace
    kcp-->>Platform: Workspace created (cluster: acc-456)

    Platform->>kcp: Create APIBinding to core.platform-mesh.io
    AccOp->>kcp: Create AccountInfo resource

    Note over SearchOp: multicluster-provider<br/>detects APIBinding

    kcp-->>SearchOp: APIBinding reconcile event
    SearchOp->>kcp: Get AccountInfo from workspace
    kcp-->>SearchOp: AccountInfo{name: dev-team, org: acme-org}

    SearchOp->>SearchOp: Build WorkspaceDocument<br/>+ OpenFGA permission tuples

    SearchOp->>OS: Index document (id: workspace-acc-456)
    OS-->>SearchOp: Indexed successfully

    Note over OS: Document now searchable<br/>with permission filtering
```

## Description
// TODO(user): An in-depth paragraph about your project and overview of use

## Getting Started

### Prerequisites
- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/search-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/search-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/search-operator:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/search-operator/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v2-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## Test Locally

Copy the `.env.example` to `.env` and replace urls:

```sh
cp .env.example .env
```

run the operator to reconcile the searchindex APIResource:

```sh
go run cmd/main.go
```

test by manually adding a searchindex resource:

```sh
export KUBCONFIG=<path to an kcp admin kubeconfig>
kubectl apply -f ./scripts/searchindex-test-resource.yaml --server="https://localhost:8443/clusters/root:orgs"
```

observe logs of successful reconciliation (start with kcp kubeconfig configured with path :root:platform-mesh-system):

```sh
# In shell:
searchindex.core.platform-mesh.io/testindex5 created
# In Operator
{"level":"info","service":"...","operator":"searchindex","controller":"SearchIndexReconciler","name":"<index name>","namespace":"","reconcile_id":"...","time":"...","caller":"...","message":"start reconcile"}
```

check if the url with your new index name in the path returns the desired values:

`https://opensearch.portal.localhost:8443/<index name>`

observe

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

