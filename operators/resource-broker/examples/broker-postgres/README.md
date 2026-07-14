# Brokering Postgres and migrating data

This example brokers the Postgres resources to consumers, backed by two providers:

1. kropg, a provider that uses kro to deploy a Deployment-based postgres instances
2. cnpg, a provider that uses kro and cnpg to deploy postgres instances

Both publish to their own workspace using their own APIExport and
register as providers with the resource-broker by binding and
instantiating the AcceptAPI resource.

## Prerequisites

- docker
- kind
- kubectl
- helm
- yq
- go
- [kcp kubectl plugins](https://docs.kcp.io/kcp/main/setup/kubectl-plugin/)

<!--
```bash ci
export PATH="${KREW_ROOT:-$HOME/.krew}/bin:$PATH"
if [[ -n "$CI" ]]; then
    _ci() {
        ./examples/broker-postgres/run.bash ci
    }
    trap _ci EXIT
fi
source ./hack/lib.bash
```
-->

## Setup

Deploy the kind clusters, providers, kcp, etc.pp.:

```bash ci
./examples/broker-postgres/run.bash setup
./examples/broker-postgres/run.bash start-broker
```

This will place a number of kubeconfigs in the `kubeconfigs` directory
for the various workspaces and clusters.

## Providers accept the API

Bind the broker's AcceptAPI APIExport for both providers:

```bash ci
for provider in kropg cnpg; do
kubectl --kubeconfig "kubeconfigs/workspaces/$provider.kubeconfig" apply -f- <<EOF
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
done
```

<!--
```bash ci
for provider in kropg cnpg; do
kubectl --kubeconfig "kubeconfigs/workspaces/$provider.kubeconfig" \
    wait --for=condition=Ready=True apibindings acceptapis --timeout=10m
done
```
-->

And create an AcceptAPI for each, with tier `dev` for kropg and tier
`production` for cnpg:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/kropg.kubeconfig apply -f- <<EOF
apiVersion: broker.platform-mesh.io/v1alpha1
kind: AcceptAPI
metadata:
  name: postgres.example.platform-mesh.io
spec:
  apiExportName: postgres
  filters:
  - key: tier
    valueIn: [dev]
  gvr:
    group: example.platform-mesh.io
    resource: postgres
    version: v1alpha1
EOF

kubectl --kubeconfig kubeconfigs/workspaces/cnpg.kubeconfig apply -f- <<EOF
apiVersion: broker.platform-mesh.io/v1alpha1
kind: AcceptAPI
metadata:
  name: postgres.example.platform-mesh.io
spec:
  apiExportName: postgres
  filters:
  - key: tier
    valueIn: [production]
  gvr:
    group: example.platform-mesh.io
    resource: postgres
    version: v1alpha1
EOF
```

```bash ci
for provider in kropg cnpg; do
kubectl --kubeconfig "kubeconfigs/workspaces/$provider.kubeconfig" wait \
    acceptapi/postgres.example.platform-mesh.io \
    --for=condition=Ready --timeout=5m
done
```

## MigrationConfiguration

Now create the MigrationConfiguration - this is what tells the broker
how to migrate a resource from one provider to another.

MigrationConfigurations are defined based on a `from` and `to` GVK. In
most cases `from` and `to` will be equal, but this allows to also handle
version up/downgrades.

This example MigrationConfiguration just deploys a pod dumping the
source postgres and pipes that to psql against the target postgres. In
production this would likely be resources backed by dedicated migration
operators.

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/broker.kubeconfig \
    apply -f ./examples/broker-postgres/migrationconfiguration.yaml
```

## Consumer orders a Postgres

In the consumer workspace bind the platforms Postgres API, which is
backed by resource-broker:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig apply -f- <<EOF
apiVersion: apis.kcp.io/v1alpha2
kind: APIBinding
metadata:
  name: postgres
spec:
  reference:
    export:
      path: root:platform
      name: postgres
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
    wait --for=condition=Ready=True apibindings postgres --timeout=10m
```
-->

And create a Postgres resource with the tier `dev`:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
    apply -f ./examples/broker-postgres/postgres.yaml
```


<!--
```bash ci
kubectl::wait::list \
    kubeconfigs/kropg.kubeconfig \
    postgres.example.platform-mesh.io \
    --all-namespaces
```
-->

This will route the resource to the kropg provider:

```bash ci
kubectl --kubeconfig kubeconfigs/kropg.kubeconfig \
    get postgres.example.platform-mesh.io --all-namespaces
```

And after a while the connection secret lands in the consumer workspace:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
    wait secret/pg-from-consumer --for=create --timeout=10m
```

Grab the connection uri and write something into a table:

```bash ci
uri="$(kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
    get secret pg-from-consumer -o jsonpath='{.data.uri}' | base64 -d)"
```

<!--
```bash ci
docker run --rm --network kind postgres:16 \
    sh -c "until pg_isready -d '$uri'; do sleep 2; done" >/dev/null
```
-->

```bash
docker run --rm --network kind postgres:16 \
    psql "$uri" -c "create table if not exists demo (msg text); insert into demo values ('hello from kropg');"
```

We'll read this back out after migrating to verify that the data has
survived the migration.

<!--
```bash ci
docker run --rm --network kind postgres:16 \
    psql "$uri" -c "create table if not exists demo (msg text); insert into demo values ('hello from kropg');"
```
-->

```bash ci
docker run --rm --network kind postgres:16 psql "$uri" -c "select * from demo;"
```

## Migration with data transfer

Now change the `tier` on the Postgres instance to production to trigger
the migration to the cnpg provider:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
    patch postgres pg-from-consumer \
        --type merge -p '{"spec":{"tier":"production"}}'
```

<!--
```bash ci
kubectl::wait::list \
    kubeconfigs/workspaces/broker.kubeconfig \
    migrations
```
-->

The Migration will only be present during the migration and will be
deleted after it was completed:

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/broker.kubeconfig \
    get migrations

kubectl --kubeconfig kubeconfigs/platform.kubeconfig \
    get jobs
```

<!--
```bash ci
kubectl::wait::empty \
    kubeconfigs/workspaces/broker.kubeconfig \
    migrations

kubectl::wait::empty \
    kubeconfigs/kropg.kubeconfig \
    postgres.example.platform-mesh.io \
    --all-namespaces
```
-->

Once the Migration has completed the instance in the kropg provider is
deleted:

```bash ci
kubectl --kubeconfig kubeconfigs/kropg.kubeconfig \
    get postgres.example.platform-mesh.io --all-namespaces
# No resources found

kubectl --kubeconfig kubeconfigs/cnpg.kubeconfig \
    get postgres.example.platform-mesh.io --all-namespaces
```

<!--
```bash ci
until kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
    get secret pg-from-consumer -o jsonpath='{.data.uri}' \
    | base64 -d | grep -q broker-cnpg-control-plane; do
    sleep 2
done
```
-->

And the connection secret now points to the cnpg cluster:

```bash ci
uri="$(kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
    get secret pg-from-consumer -o jsonpath='{.data.uri}' | base64 -d)"

docker run --rm --network kind postgres:16 \
    psql "$uri" \
    -c "select * from demo;"
```

## Cleanup

Finally, cleanup:

```bash noci
./examples/broker-postgres/run.bash cleanup
./examples/broker-postgres/run.bash stop-broker
```

Or delete the clusters:

```bash noci
kind delete cluster --name broker-platform
kind delete cluster --name broker-kropg
kind delete cluster --name broker-cnpg
```
