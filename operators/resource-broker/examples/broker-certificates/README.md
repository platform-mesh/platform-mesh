# Brokering Certificates with kcp

This example uses resource-broker to broker Certificate resources from
a consumer to matching providers, utilizing the isolation and API
sharing capabilities of [kcp](https://kcp.io/).

## Overview

This example uses kcp as a control plane layer that:

1. Uses kcp workspaces to isolate consumers and providers
2. Shares APIs between workspaces using APIExports and APIBindings instead of installing CRDs manually
3. Synchronizes resources between kcp workspaces and compute clusters using [api-syncagent](https://docs.kcp.io/api-syncagent/main/)

## Prerequisites

### Required Tools

- docker
- kind
- kubectl
- helm
- yq
- go
- [kcp kubectl plugins](https://docs.kcp.io/kcp/main/setup/kubectl-plugin/)

<!--
TODO(ntnn): Install kubectl plugins locally via e.g. uget
For now just add the krew bin folder to path
```bash ci
export PATH="${KREW_ROOT:-$HOME/.krew}/bin:$PATH"
if [[ -n "$CI" ]]; then
    _ci() {
        ./examples/broker-certificates/run.bash ci
    }
    trap _ci EXIT
fi
```
-->

## Components

### Platform Cluster

The platform cluster hosts kcp and the resource-broker.

#### Platform kcp Workspace

The platform kcp workspace exports the AcceptAPI for providers and the
generic Certificate API for consumers.

#### Broker kcp Workspaces

The broker workspace (`root:platform:broker`) stores the broker's own
state as regular resources instead of in-memory maps:

`Assignment` resources pin a consumer resource to the provider it was routed to.

`StagingWorkspace` resources describe the staging workspaces the broker maintains.

Its children serve as tree roots for the workspaces the broker creates:

`root:platform:broker:verification` holds `verify-{hash}` workspaces,
one per provider APIExport, in which the broker verifies that an
AcceptAPI's referenced APIExport is bindable.

`root:platform:broker:staging` holds `staging-{hash}` workspaces, one
per consumer/provider/APIExport tuple, into which the broker copies
consumer resources for the provider to serve.

### Consumer kcp Workspace

The consumer workspace binds the Certificate generic API from the
platform workspace. When creating an instance the resource-broker will
be able to see and interact with it through the [Virtual Workspace](https://docs.kcp.io/kcp/main/concepts/workspaces/virtual-workspaces/) of the APIExport.

### Provider Clusters (InternalCA & ExternalCA)

The provider compute clusters run kro and cert-manager to issue certificates.
They publish their certificate API to their respective kcp workspaces using api-syncagent.

#### Provider kcp Workspaces

The provider workspaces bind the AcceptAPI from the platform workspace
and create an AcceptAPI resource to declare under which constraints they
will be able to serve Certificate resources from consumers.
The resource-broker sees these AcceptAPIs through the Virtual Workspace of the APIExport.

Additionally they create APIExports of their own published Certificate
API (synced from the compute cluster with api-syncagent) and bind them
in their own workspace so api-syncagent has a Virtual Workspace to sync
through. The resource-broker binds these APIExports in its verification
and staging workspaces.

<!--
```bash ci
# source the library so ci can use the functions
source ./hack/lib.bash
```
-->

## Running the Example

### Setup

Setup the kind clusters and install components (kcp, cert-manager, etcd, ...).
Kubeconfig files for clusters and workspaces will be created in the `./kubeconfigs/` directory.

The setup also creates two APIExports in the platform workspace:

1. AcceptAPI, which providers bind to declare which APIs they can serve
   and under which constraints
2. Certificate API, which consumers bind to create Certificate resources

Additionally it creates the broker workspace `root:platform:broker`
(holding the Assignment and StagingWorkspace CRDs for the broker's
routing state) with its `staging` and `verification` child workspaces.

The resource-broker routes Certificate resources from consumers to
a provider depending on the constraints declared by the providers.

```bash ci
./examples/broker-certificates/run.bash setup
```

Build and start the resource-broker in the platform cluster:

<!-- TODO(ntnn): use operator and prebuilt docker image and include in the setup -->
```bash ci
./examples/broker-certificates/run.bash start-broker
```

> [!NOTE]
> At any point you can run `./examples/broker-certificates/run.bash cleanup` to get back to this state.

### Example

#### Setting up Providers

The setup script already deployed api-syncagent to publish APIs from the
provider clusters to their respective kcp workspaces.

Now bind the AcceptAPI from the platform workspace into the internalca provider workspace:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/internalca.kubeconfig apply -f- <<EOF
apiVersion: apis.kcp.io/v1alpha2
kind: APIBinding
metadata:
  name: acceptapis
spec:
  reference:
    export:
      path: root:platform
      name: acceptapis
  permissionClaims:
    - resource: secrets
      group: ""
      state: Accepted
      selector:
        matchAll: true
      verbs:
        - get
        - list
        - watch
EOF
```

<!--
```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/internalca.kubeconfig \
    wait --for=condition=Ready=True apibindings acceptapis --timeout=10m
```
-->

And do the same for the externalca provider workspace:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/externalca.kubeconfig apply -f- <<EOF
apiVersion: apis.kcp.io/v1alpha2
kind: APIBinding
metadata:
  name: acceptapis
spec:
  reference:
    export:
      path: root:platform
      name: acceptapis
  permissionClaims:
    - resource: secrets
      group: ""
      state: Accepted
      selector:
        matchAll: true
      verbs:
        - get
        - list
        - watch
EOF
```

<!--
```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/externalca.kubeconfig \
    wait --for=condition=Ready=True apibindings acceptapis --timeout=10m
```
-->

And now create AcceptAPI resources in both provider workspaces.

The internalca provider will accept Certificate resources with `spec.fqdn` ending with `internal.corp`:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/internalca.kubeconfig apply -f- <<EOF
apiVersion: broker.platform-mesh.io/v1alpha1
kind: AcceptAPI
metadata:
  name: certificates.example.platform-mesh.io
spec:
  apiExportName: certificates
  filters:
  - key: fqdn
    suffix: internal.corp
  gvr:
    group: example.platform-mesh.io
    resource: certificates
    version: v1alpha1
EOF
```

`spec.apiExportName` names the APIExport in the provider's workspace
that serves the accepted resources. The resource-broker verifies that
the export is bindable and binds it in the staging workspaces it routes
consumer resources through.

Now create the AcceptAPI for the externalca provider, which will
accept Certificate resources with `spec.fqdn` ending with `corp.com`:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/externalca.kubeconfig apply -f- <<EOF
apiVersion: broker.platform-mesh.io/v1alpha1
kind: AcceptAPI
metadata:
  name: certificates.example.platform-mesh.io
spec:
  apiExportName: certificates
  filters:
  - key: fqdn
    suffix: corp.com
  gvr:
    group: example.platform-mesh.io
    resource: certificates
    version: v1alpha1
EOF
```

Once the broker has verified that the referenced APIExports are
bindable the AcceptAPIs report Ready:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/internalca.kubeconfig wait \
    acceptapi/certificates.example.platform-mesh.io \
    --for=condition=Ready --timeout=5m
kubectl --kubeconfig kubeconfigs/workspaces/externalca.kubeconfig wait \
    acceptapi/certificates.example.platform-mesh.io \
    --for=condition=Ready --timeout=5m
```

With the providers set up resource-broker can now route Certificate
resources from consumers.

#### Consumer binding the generic Certificate API

resource-broker uses kcp's [API exporting and binding](https://docs.kcp.io/kcp/v0.29/concepts/apis/exporting-apis/) to implement generic APIs.

The generic APIs defined by the platform are exported using [APIExports](https://docs.kcp.io/kcp/v0.29/reference/crd/apis.kcp.io/apiexports/). Consumers can bind these APIs into their own workspaces using [APIBindings](https://docs.kcp.io/kcp/v0.29/reference/crd/apis.kcp.io/apibindings/).

Checking the available APIs in the consumer workspace there are no
Certificates available yet:

```bash ci
kubectl --kubeconfig="./kubeconfigs/workspaces/consumer.kubeconfig" api-resources --api-group example.platform-mesh.io
```


Now bind the certificate APIExport from the platform workspace into the
consumer workspace, accepting the permission claims the broker needs to
copy related resources into the workspace:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig apply -f- <<EOF
apiVersion: apis.kcp.io/v1alpha2
kind: APIBinding
metadata:
  name: certificates
spec:
  reference:
    export:
      path: root:platform
      name: certificates
  permissionClaims:
    - resource: secrets
      group: ""
      state: Accepted
      selector:
        matchAll: true
      verbs:
        - '*'
    - resource: events
      group: ""
      state: Accepted
      selector:
        matchAll: true
      verbs:
        - '*'
    - resource: namespaces
      group: ""
      state: Accepted
      selector:
        matchAll: true
      verbs:
        - '*'
EOF
```

<!--
```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
    wait --for=condition=Ready=True apibindings certificates --timeout=10m
```
-->

The same binding can also be created with the `kubectl kcp bind
apiexport` CLI convenience, which builds the manifest and waits for the
binding to become ready.

Inspect the resulting APIBinding in the consumer workspace:

```bash ci
kubectl --kubeconfig="./kubeconfigs/workspaces/consumer.kubeconfig" get apibindings certificates -o yaml
```

After binding the Certificate resource is available in the consumer workspace:

```bash ci
kubectl --kubeconfig="./kubeconfigs/workspaces/consumer.kubeconfig" api-resources --api-group example.platform-mesh.io
```

#### Ordering a Certificate

Create a Certificate resource in the consumer workspace:

```yaml
apiVersion: example.platform-mesh.io/v1alpha1
kind: Certificate
metadata:
  name: cert-from-consumer
spec:
  fqdn: app.internal.corp
```

```bash ci
kubectl --kubeconfig="./kubeconfigs/workspaces/consumer.kubeconfig" apply -f ./examples/broker-certificates/cert.yaml
```

The resource-broker sees the Certificate in the virtual workspace of the
APIExport and routes it to a matching provider: it creates an Assignment
pinning the Certificate to the provider, ensures a staging workspace
bound to the provider's APIExport and copies the Certificate there.
Since the fqdn is `app.internal.corp` the InternalCA provider will issue
the certificate.

> [!NOTE]
> The staging copy keeps the consumer-side namespace and name
> (`default/cert-from-consumer`). The mangled names visible on the
> provider's compute cluster below come from the api-syncagent, which
> rewrites names when syncing from the provider workspace to the
> compute cluster.

Wait for the certificate to appear on the internalca provider cluster:

<!--
```bash ci
kubectl::wait::list \
    kubeconfigs/internalca.kubeconfig \
    certificates.example.platform-mesh.io \
    --all-namespaces
```
-->

```bash
kubectl --kubeconfig kubeconfigs/internalca.kubeconfig get certificates.example.platform-mesh.io --all-namespaces
```

```
NAMESPACE          NAME                                        STATE    SYNCED   AGE
9n832d7e4xebepg1   2747cabbb481a433679f-42b4d6246cf320c6cee5   ACTIVE   True     10m
```

```bash ci
kubectl --kubeconfig kubeconfigs/internalca.kubeconfig get certificates.example.platform-mesh.io --all-namespaces -o yaml
```

```yaml
apiVersion: v1
items:
- apiVersion: example.platform-mesh.io/v1alpha1
  kind: Certificate
  metadata:
    # ...
    name: 2747cabbb481a433679f-42b4d6246cf320c6cee5
    namespace: 9n832d7e4xebepg1
    # ...
  spec:
    fqdn: app.internal.corp
  status:
    # ...
    relatedResources:
      secret:
        gvk:
          group: core
          kind: Secret
          version: v1
        name: cert-from-consumer
        namespace: default
    # ...
kind: List
metadata:
  resourceVersion: ""
```

The provider has created a cert-manager Certificate, which in turn
generated a Secret with the issued certificate:

```bash ci
kubectl --kubeconfig kubeconfigs/internalca.kubeconfig \
    get secrets -A -l kro.run/resource-graph-definition-name=certificates.example.platform-mesh.io
```

<!--
```bash ci
cert_namespace="$(kubectl --kubeconfig kubeconfigs/internalca.kubeconfig get certificates.example.platform-mesh.io --all-namespaces -o jsonpath="{.items[0].metadata.namespace}")"
kubectl::wait::cert::subject \
    kubeconfigs/internalca.kubeconfig \
    "cert-from-consumer" \
    "$cert_namespace" \
    "app.internal.corp"
```
-->


Decoding the `tls.crt` field shows the certificate was correctly issued for `app.internal.corp`:

```bash ci
kubectl --kubeconfig kubeconfigs/internalca.kubeconfig get secrets -A -l kro.run/resource-graph-definition-name=certificates.example.platform-mesh.io  -o jsonpath='{.items[0].data.tls\.crt}' \
    | base64 --decode \
    | openssl x509 -noout -subject
# subject=CN=app.internal.corp
```

Wait for the certificate secret to be synced to the consumer workspace:

<!--
```bash ci
kubectl::wait::cert::subject \
    kubeconfigs/workspaces/consumer.kubeconfig \
    "cert-from-consumer" \
    "default" \
    "app.internal.corp"
```
-->

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
    wait secret/cert-from-consumer --for=create --timeout=5m
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig get secrets "cert-from-consumer"
```

And comparing the serial number shows it's the same certificate:

```bash ci
kubectl --kubeconfig kubeconfigs/internalca.kubeconfig \
    get secrets -A -l kro.run/resource-graph-definition-name=certificates.example.platform-mesh.io  -o jsonpath='{.items[0].data.tls\.crt}' \
    | base64 --decode \
    | openssl x509 -noout -serial
# serial=0E7311D15E34081A8F1FD7447F1FF4C7BC055238
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
    get secrets "cert-from-consumer" \
        -o jsonpath="{.data.tls\.crt}" \
    | base64 --decode \
    | openssl x509 -noout -serial
# serial=0E7311D15E34081A8F1FD7447F1FF4C7BC055238
```

#### Switching providers

Now update the Certificate to request a certificate for `app.corp.com`,
which should be issued by the externalca provider.

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
    patch certificates cert-from-consumer \
        --type merge -p '{"spec":{"fqdn":"app.corp.com"}}'
```

The new fqdn no longer matches the internalca provider's AcceptAPI filter.
The resource-broker notices this, creates a Migration in the broker
workspace and stages the Certificate in a second staging workspace bound
to the externalca provider. Both providers serve the Certificate during
the migration; once the externalca copy reports itself as available the
broker points the assignment at the externalca provider and removes the
copy from the internalca provider.

A MigrationConfiguration in the broker workspace can insert verification
stages between providers before the cutover happens, e.g. to migrate
data. This example uses the direct cutover without stages.

Just like with the internalca provider, the Certificate shows up in the externalca provider cluster:

```bash ci
kubectl --kubeconfig kubeconfigs/externalca.kubeconfig get certificates.example.platform-mesh.io --all-namespaces
```

The internalca and externalca providers have the same setup, with KRO
relaying the Certificate example resource to a cert-manager Certificate
and back, so the secret name and namespace can be grabbed the same way:

<!--
```bash ci
kubectl::wait::list \
    kubeconfigs/externalca.kubeconfig \
    certificates.example.platform-mesh.io \
    --all-namespaces
```
-->

```bash ci
kubectl --kubeconfig kubeconfigs/externalca.kubeconfig \
    get secrets -A -l kro.run/resource-graph-definition-name=certificates.example.platform-mesh.io
```

<!--
```bash ci
cert_namespace="$(kubectl --kubeconfig kubeconfigs/externalca.kubeconfig get certificates.example.platform-mesh.io --all-namespaces -o jsonpath="{.items[0].metadata.namespace}")"
kubectl::wait::cert::subject \
    kubeconfigs/externalca.kubeconfig \
    "cert-from-consumer" \
    "$secret_namespace" \
    "app.corp.com"
```
-->

And decoding the `tls.crt` field shows the certificate was correctly issued for `app.corp.com`:

```bash ci
kubectl --kubeconfig kubeconfigs/externalca.kubeconfig get secrets -A -l kro.run/resource-graph-definition-name=certificates.example.platform-mesh.io  -o jsonpath='{.items[0].data.tls\.crt}' \
    | base64 --decode \
    | openssl x509 -noout -subject
# subject=CN=app.corp.com
```

<!--
```bash ci
kubectl::wait::cert::subject \
    kubeconfigs/workspaces/consumer.kubeconfig \
    "cert-from-consumer" \
    "default" \
    "app.corp.com"
```
-->

And the secret in the consumer workspace has been updated accordingly:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
    get secrets "cert-from-consumer" \
        -o jsonpath="{.data.tls\.crt}" \
    | base64 --decode \
    | openssl x509 -noout -subject
# subject=CN=app.corp.com
```

And again comparing the serial numbers, now with the certificate in the externalca cluster, shows it's the same certificate:

```bash ci
kubectl --kubeconfig kubeconfigs/externalca.kubeconfig \
    get secrets -A -l kro.run/resource-graph-definition-name=certificates.example.platform-mesh.io  -o jsonpath='{.items[0].data.tls\.crt}' \
    | base64 --decode \
    | openssl x509 -noout -serial
# serial=204F68FCA700404CB7745D7A603BA5A28DC68E95
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
    get secrets "cert-from-consumer" \
        -o jsonpath="{.data.tls\.crt}" \
    | base64 --decode \
    | openssl x509 -noout -serial
# serial=204F68FCA700404CB7745D7A603BA5A28DC68E95
```

Once the migration has cut over, the broker removes the Certificate from
the internalca provider:

<!--
```bash ci
kubectl::wait::empty \
    kubeconfigs/internalca.kubeconfig \
    certificates.example.platform-mesh.io \
    --all-namespaces
```
-->

```bash
kubectl --kubeconfig kubeconfigs/internalca.kubeconfig get certificates.example.platform-mesh.io --all-namespaces
# No resources found
```

### Cleanup

4. (Optional) Clean up resources created during the example

```bash noci
./examples/broker-certificates/run.bash cleanup
./examples/broker-certificates/run.bash stop-broker
```

Or delete the clusters:

```bash noci
kind delete cluster --name broker-platform
kind delete cluster --name broker-internalca
kind delete cluster --name broker-externalca
```
