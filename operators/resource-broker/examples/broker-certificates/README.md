# Brokering Certificates with kcp

This example uses resource-broker to broker Certificate resources from
a consumer to matching providers, utilizing the isolation and API
sharing capabilities of [kcp](https://kcp.io/).

## Overview

While the basic certs example uses direct kubeconfig access to multiple clusters, this example introduces kcp as a control plane layer that:

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

Additionally they are creating APIExports of their own published
Certificate API (synced from the compute cluster with api-syncagent) and
bind them in their own workspace to get a Virtual Workspace for the
platform to use.

<!-- TODO: This hackery could be too complex for new users. -->

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
kcp::apibinding kubeconfigs/workspaces/internalca.kubeconfig \
    "root:platform" acceptapis \
    secrets "" 'get,list,watch'
```

And do the same for the externalca provider workspace:

```bash ci
kcp::apibinding kubeconfigs/workspaces/externalca.kubeconfig \
    "root:platform" acceptapis \
    secrets "" 'get,list,watch'
```

And now create AcceptAPI resources in both provider workspaces.

The internalca provider will accept Certificate resources with `spec.fqdn` ending with `internal.corp`:

<!--
TODO(ntnn): replace the annotation with giving the resource-broker
service account access properly using RBAC.
Maybe let resource-broker bind the APIExport from the provider when
reconciling the AcceptAPI?
-->

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/internalca.kubeconfig apply -f- <<EOF
apiVersion: broker.platform-mesh.io/v1alpha1
kind: AcceptAPI
metadata:
  annotations:
    broker.platform-mesh.io/kcp-apiexport-name: certificates
  name: certificates.example.platform-mesh.io
spec:
  filters:
  - key: fqdn
    suffix: internal.corp
  gvr:
    group: example.platform-mesh.io
    resource: certificates
    version: v1alpha1
EOF
```

The annotation `broker.platform-mesh.io/kcp-apiexport-name` tells the
resource-broker which APIExport on the provider workspace to use for
routing.

Now create the AcceptAPI for the externalca provider, which will
accept Certificate resources with `spec.fqdn` ending with `corp.com`:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/externalca.kubeconfig apply -f- <<EOF
apiVersion: broker.platform-mesh.io/v1alpha1
kind: AcceptAPI
metadata:
  annotations:
    broker.platform-mesh.io/kcp-apiexport-name: certificates
  name: certificates.example.platform-mesh.io
spec:
  filters:
  - key: fqdn
    suffix: corp.com
  gvr:
    group: example.platform-mesh.io
    resource: certificates
    version: v1alpha1
EOF
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


Now bind the certificate APIExport from the platform workspace into the consumer workspace:

```bash ci
kcp::apibinding kubeconfigs/workspaces/consumer.kubeconfig \
    "root:platform" certificates \
    secrets "" '*' \
    events "" '*' \
    namespaces "" '*'
```

This will create an APIBinding in the consumer workspace:

```bash ci
kubectl --kubeconfig="./kubeconfigs/workspaces/consumer.kubeconfig" get apibindings certificates -o yaml
```

Running `kubectl kcp bind apiexport ...` is equivalent to just applying
this manifest:

```yaml
apiVersion: apis.kcp.io/v1alpha2
kind: APIBinding
metadata:
  name: certificates
spec:
  reference:
    export:
      name: certificates
      path: root:platform
  permissionClaims:
  - group: ""
    identityHash: ""
    resource: secrets
    selector:
      matchAll: true
    state: Accepted
    verbs:
    - '*'
  - group: ""
    identityHash: ""
    resource: events
    selector:
      matchAll: true
    state: Accepted
    verbs:
    - '*'
  - group: ""
    identityHash: ""
    resource: namespaces
    selector:
      matchAll: true
    state: Accepted
    verbs:
    - '*'
```

In automated deployments the manifest is the way to go. The CLI is
merely a convenience as it builds the manifest and waits for the binding
to be ready.

After binding the Certificate resource is available in the consumer workspace:

```bash ci
kubectl --kubeconfig="./kubeconfigs/workspaces/consumer.kubeconfig" api-resources --api-group example.platform-mesh.io
```

<!--
```bash ci
if kubectl --kubeconfig="./kubeconfigs/workspaces/consumer.kubeconfig" api-resources --api-group example.plastform-mesh.io | grep -q Certificate; then
    echo "Certificate API should not be available yet"
    exit 1
fi
```
-->

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

The resource-broker will see the Certificate in the virtual workspace of the APIExport, pass it to a matching provider. Since the fqdn is `app.internal.corp` the InternalCA provider will issue the certificate.

> [!NOTE]
> Provider-side resource names are prefixed with a hash of the consumer cluster name to prevent
> naming conflicts when multiple consumers create resources with the same name.
> The original consumer-side name `cert-from-consumer` becomes `{hash}-cert-from-consumer` in
> the provider's virtual workspace.

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
        name: {hash}-cert-from-consumer
        namespace: default
    # ...
kind: List
metadata:
  resourceVersion: ""
```

Grab the name and namespace from the compute cluster:

```bash ci
cert_name="$(kubectl --kubeconfig kubeconfigs/internalca.kubeconfig get certificates.example.platform-mesh.io --all-namespaces -o jsonpath="{.items[0].metadata.name}")"
cert_namespace="$(kubectl --kubeconfig kubeconfigs/internalca.kubeconfig get certificates.example.platform-mesh.io --all-namespaces -o jsonpath="{.items[0].metadata.namespace}")"
kubectl --kubeconfig kubeconfigs/internalca.kubeconfig wait \
    "certificates.example.platform-mesh.io/$cert_name" \
    --namespace="$cert_namespace" \
    --for=jsonpath="{.status.relatedResources.secret.name}" \
    --timeout=5m
secret_name="$(kubectl --kubeconfig kubeconfigs/internalca.kubeconfig get "certificates.example.platform-mesh.io/$cert_name" --namespace="$cert_namespace" -o jsonpath="{.status.relatedResources.secret.name}")"
secret_namespace="$cert_namespace"
```

<!--
```bash ci
kubectl::wait::cert::subject \
    kubeconfigs/internalca.kubeconfig \
    "$secret_name" \
    "$secret_namespace" \
    "app.internal.corp"
```
-->

The provider has created a cert-manager Certificate, which in turn
generated a Secret with the issued certificate:

```bash ci
kubectl --kubeconfig kubeconfigs/internalca.kubeconfig get secrets --namespace "$secret_namespace"
```

Decoding the `tls.crt` field shows the certificate was correctly issued for `app.internal.corp`:

```bash ci
kubectl --kubeconfig kubeconfigs/internalca.kubeconfig get secrets --namespace "$secret_namespace" "$secret_name" -o jsonpath="{.data.tls\.crt}" \
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
    get secrets --namespace "$secret_namespace" "$secret_name" \
        -o jsonpath="{.data.tls\.crt}" \
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

Just like with the internalca provider, the Certificate shows up in the externalca provider cluster:

```bash ci
kubectl --kubeconfig kubeconfigs/externalca.kubeconfig \
    get certificates.example.platform-mesh.io --all-namespaces
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
cert_name="$(kubectl --kubeconfig kubeconfigs/externalca.kubeconfig get certificates.example.platform-mesh.io --all-namespaces -o jsonpath="{.items[0].metadata.name}")"
cert_namespace="$(kubectl --kubeconfig kubeconfigs/externalca.kubeconfig get certificates.example.platform-mesh.io --all-namespaces -o jsonpath="{.items[0].metadata.namespace}")"
kubectl --kubeconfig kubeconfigs/externalca.kubeconfig wait \
    "certificates.example.platform-mesh.io/$cert_name" \
    --namespace="$cert_namespace" \
    --for=jsonpath="{.status.relatedResources.secret.name}" \
    --timeout=5m
secret_name="$(kubectl --kubeconfig kubeconfigs/externalca.kubeconfig get "certificates.example.platform-mesh.io/$cert_name" --namespace="$cert_namespace" -o jsonpath="{.status.relatedResources.secret.name}")"
secret_namespace="$cert_namespace"
```

<!--
```bash ci
kubectl::wait::cert::subject \
    kubeconfigs/externalca.kubeconfig \
    "$secret_name" \
    "$secret_namespace" \
    "app.corp.com"
```
-->

And decoding the `tls.crt` field shows the certificate was correctly issued for `app.corp.com`:

```bash ci
kubectl --kubeconfig kubeconfigs/externalca.kubeconfig \
    get secrets --namespace "$secret_namespace" "$secret_name" \
        -o jsonpath="{.data.tls\.crt}" \
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
    get secrets --namespace "$secret_namespace" "$secret_name" \
        -o jsonpath="{.data.tls\.crt}" \
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
