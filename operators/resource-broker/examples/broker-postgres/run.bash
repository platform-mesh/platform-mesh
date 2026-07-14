#!/usr/bin/env bash

# cd into repo root
example_dir="$(realpath "$(dirname "$0")")"
cd "$example_dir/../.."
source "./hack/lib.bash"

if [[ -n "$CI" ]]; then
    # In CI, install kcp plugins
    kcp::setup::plugins
fi

kubeconfigs="$PWD/kubeconfigs"
log "Using directory for kubeconfigs: $kubeconfigs"

workspace_kubeconfigs="$kubeconfigs/workspaces"
mkdir -p "$workspace_kubeconfigs"

kind_platform="$kubeconfigs/platform.kubeconfig"
ws_platform="$workspace_kubeconfigs/platform.kubeconfig"
ws_broker="$workspace_kubeconfigs/broker.kubeconfig"
ws_staging="$workspace_kubeconfigs/staging.kubeconfig"
ws_verification="$workspace_kubeconfigs/verification.kubeconfig"

kind_kropg="$kubeconfigs/kropg.kubeconfig"
ws_kropg="$workspace_kubeconfigs/kropg.kubeconfig"

kind_cnpg="$kubeconfigs/cnpg.kubeconfig"
ws_cnpg="$workspace_kubeconfigs/cnpg.kubeconfig"

ws_consumer="$workspace_kubeconfigs/consumer.kubeconfig"

_setup() {
    log "Setting up platform cluster"
    kind::cluster platform "$kind_platform"
    # The migration stage Job runs postgres:16 on the platform cluster.
    kind::load::image platform postgres:16
    helm::install::certmanager "$kind_platform"
    helm::install::etcddruid "$kind_platform"
    helm::install::kcp "$kind_platform"

    log "Setting up kcp"
    kubectl::kustomize "$kind_platform" "./examples/broker-postgres/platform"
    kcp::setup::kubeconfigs \
        "$kind_platform" \
        "$kubeconfigs/kcp-admin.kubeconfig" \
        "$kubeconfigs/kcp-from-host.kubeconfig"

    log "Setting up platform kcp workspace"
    kcp::create_workspace "$kubeconfigs/kcp-admin.kubeconfig" "$ws_platform" "platform"

    log "Setting up broker kcp workspaces"
    # root:platform:broker doubles as the coordination workspace, with
    # the staging and verification tree roots as children.
    kcp::create_workspace "$ws_platform" "$ws_broker" "broker"
    kcp::create_workspace "$ws_broker" "$ws_staging" "staging"
    kcp::create_workspace "$ws_broker" "$ws_verification" "verification"

    log "Installing coordination CRDs into broker workspace"
    kubectl::apply \
        "$ws_broker" \
        ./config/coordbroker/crd/coord.broker.platform-mesh.io_assignments.yaml \
        ./config/coordbroker/crd/coord.broker.platform-mesh.io_migrationconfigurations.yaml \
        ./config/coordbroker/crd/coord.broker.platform-mesh.io_migrations.yaml \
        ./config/coordbroker/crd/coord.broker.platform-mesh.io_stagingworkspaces.yaml

    log "Setting up AcceptAPI APIExport for providers"
    kcp::apiexport "$ws_platform" ./config/broker/crd/broker.platform-mesh.io_acceptapis.yaml \
        secrets get,list,watch

    log "Setting up Postgres APIExport for consumers"
    kcp::apiexport "$ws_platform" ./config/example/crd/example.platform-mesh.io_postgres.yaml \
        secrets '*' \
        events '*' \
        namespaces '*'

    log "Setting up kropg provider"
    _provider_setup kropg "$kind_kropg" "$ws_kropg"

    log "Setting up cnpg provider"
    _provider_setup cnpg "$kind_cnpg" "$ws_cnpg"

    log "Setting up consumer kcp workspace"
    kcp::create_workspace "$kubeconfigs/kcp-admin.kubeconfig" "$ws_consumer" "consumer"
}

_provider_setup() {
    local name="$1"
    local kind_kubeconfig="$2"
    local ws_kubeconfig="$3"

    kcp::create_workspace "$kubeconfigs/kcp-admin.kubeconfig" "$ws_kubeconfig" "$name"

    log "Creating APIExport postgres in $name workspace"
    {
        echo "apiVersion: apis.kcp.io/v1alpha1"
        echo "kind: APIExport"
        echo "metadata:"
        echo "  name: postgres"
    } | kubectl::apply "$ws_kubeconfig" "-"

    log "Setting up $name kind cluster"
    kind::cluster "$name" "$kind_kubeconfig"
    helm::install::kro "$kind_kubeconfig"
    if [[ "$name" == cnpg ]]; then
        helm::install::cnpg "$kind_kubeconfig"
        kind::load::image "$name" ghcr.io/cloudnative-pg/postgresql:16
    else
        kind::load::image "$name" postgres:16
    fi
    kubectl::kustomize "$kind_kubeconfig" "$example_dir/$name"
    kubectl::wait "$kind_kubeconfig" rgd/postgres.example.platform-mesh.io "" create
    kubectl::wait "$kind_kubeconfig" rgd/postgres.example.platform-mesh.io "" condition=Ready

    log "Setting up api-syncagent in $name kind cluster"

    local api_syncagent_token="$(kcp::serviceaccount::admin "$ws_kubeconfig" api-syncagent default)"
    local api_syncagent_kubeconfig="$kubeconfigs/api-syncagent-$name.kubeconfig"
    kubeconfig::create::token "$api_syncagent_kubeconfig" \
        "$(kubectl::kubeconfig::current_server_url "$ws_kubeconfig")" \
        "$api_syncagent_token"
    kubectl::kubeconfig::secret "$kind_kubeconfig" "$api_syncagent_kubeconfig" "$name" default "broker-platform-control-plane:32111"

    helm::install::api_syncagent "$kind_kubeconfig" "postgres" "$name" "kubeconfig-$name" \
        --set replicas=1

    {
        echo "apiVersion: syncagent.kcp.io/v1alpha1"
        echo "kind: PublishedResource"
        echo "metadata:"
        echo "  name: postgres.example.platform-mesh.io"
        echo "spec:"
        echo "  resource:"
        echo "    kind: Postgres"
        echo "    apiGroup: example.platform-mesh.io"
        echo "    versions: [v1alpha1]"
        echo "  projection:"
        echo "    plural: postgres"
        echo "  related:"
        echo "    - identifier: credentials"
        echo "      origin: service"
        echo "      kind: Secret"
        echo "      object:"
        echo "        reference:"
        echo "          path: status.relatedResources.credentials.name"
        echo "---"
        echo "apiVersion: rbac.authorization.k8s.io/v1"
        echo "kind: ClusterRole"
        echo "metadata:"
        echo "  name: api-syncagent-$name:postgres"
        echo "rules:"
        echo "  - apiGroups:"
        echo "      - example.platform-mesh.io"
        echo "    resources:"
        echo "      - postgres"
        echo "      - postgreses"
        echo "    verbs:"
        echo "      - get"
        echo "      - list"
        echo "      - watch"
        echo "      - create"
        echo "      - update"
        echo "      - delete"
        echo "      - patch"
        echo "---"
        echo "apiVersion: rbac.authorization.k8s.io/v1"
        echo "kind: ClusterRoleBinding"
        echo "metadata:"
        echo "  name: api-syncagent-$name:postgres"
        echo "roleRef:"
        echo "  apiGroup: rbac.authorization.k8s.io"
        echo "  kind: ClusterRole"
        echo "  name: api-syncagent-$name:postgres"
        echo "subjects:"
        echo "  - kind: ServiceAccount"
        echo "    name: api-syncagent-$name"
        echo "    namespace: default"
    } | kubectl::apply "$kind_kubeconfig" -
}

_start_broker() {
    log "Starting broker"

    log "Deploy resource-broker"
    if [[ -z "$CI" ]]; then
        task docker-build || die "Failed to build resource-broker docker image"
    fi
    task kind-load KIND_CLUSTER=broker-platform \
        || die "Failed to load resource-broker image into kind cluster"

    # Grab the new kubeconfig for the broker, targeting the platform
    # workspace. This will be mounted into the resource-broker pod.
    KUBECONFIG="$kind_platform" \
        kubectl get secret operator-kubeconfig -o jsonpath='{.data.kubeconfig}' \
            | base64 -d \
            > "$kubeconfigs/operator.kubeconfig" \
            || die "Failed to get operator kubeconfig from kind cluster"
    yq -i '(.clusters[] | select(.name=="default") | .cluster.server) += ":platform"' \
        "$kubeconfigs/operator.kubeconfig" \
        || die "Failed to modify operator kubeconfig server URL"

    kubectl::kustomize "$kind_platform" "./examples/broker-postgres/platform/broker"

    kubectl create secret generic kcp-kubeconfig --namespace=resource-broker-system --dry-run=client -o yaml \
        --from-file=kubeconfig="$kubeconfigs/operator.kubeconfig" \
        | kubectl::apply "$kind_platform" "-"

    kubectl::wait "$kind_platform" deployment/resource-broker resource-broker-system condition=Available
}

_stop_broker() {
    kubectl --kubeconfig "$kind_platform" delete -k ./examples/broker-postgres/platform/broker
}

_cleanup() {
    log "Cleaning up example resources in consumer ws"
    kubectl::delete "$ws_consumer" postgres.example.platform-mesh.io/pg-from-consumer

    log "Cleaning up MigrationConfiguration in broker ws"
    kubectl --kubeconfig "$ws_broker" delete --ignore-not-found migrationconfigurations --all

    log "Cleaning up AcceptAPIs in provider workspaces"
    kubectl --kubeconfig "$ws_kropg" delete --ignore-not-found acceptapis --all
    kubectl --kubeconfig "$ws_cnpg" delete --ignore-not-found acceptapis --all

    # api-syncagent creates its own names and namespaces on the provider
    # clusters, so query them for leftovers.
    local provider_pg provider_ns kind_kubeconfig ws_kubeconfig
    for name in kropg cnpg; do
        kind_kubeconfig="$kubeconfigs/$name.kubeconfig"
        ws_kubeconfig="$workspace_kubeconfigs/$name.kubeconfig"

        log "Cleaning up example resources in $name"
        provider_pg="$(kubectl --kubeconfig "$kind_kubeconfig" get postgres.example.platform-mesh.io -A -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)" || true
        provider_ns="$(kubectl --kubeconfig "$kind_kubeconfig" get postgres.example.platform-mesh.io -A -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null)" || true
        if [[ -n "$provider_pg" ]]; then
            kubectl --kubeconfig "$kind_kubeconfig" delete --ignore-not-found -n "$provider_ns" \
                "postgres.example.platform-mesh.io/$provider_pg"
            kubectl --kubeconfig "$kind_kubeconfig" delete --ignore-not-found -n "$provider_ns" \
                --selector kro.run/owned=true secrets
        fi

        log "Cleaning up Postgres APIBindings and APIExports in $name"
        kubectl --kubeconfig "$ws_kubeconfig" delete apiexport postgres
        kubectl --kubeconfig "$ws_kubeconfig" delete apibinding acceptapis
    done

    kubectl --kubeconfig "$ws_consumer" delete apibinding postgres

    return 0
}

_ci() {
    # Collect logs for debugging - ignore errors since resources may not exist if test failed early
    kubectl --kubeconfig "$kind_platform" logs -n resource-broker-system deployment/resource-broker > resource-broker.log 2>&1 || true
    kubectl --kubeconfig "$ws_consumer" get postgres.example.platform-mesh.io pg-from-consumer -o yaml > consumer-postgres.yaml 2>&1 || true

    kubectl --kubeconfig "$ws_broker" get assignments -o yaml > broker-assignments.yaml 2>&1 || true
    kubectl --kubeconfig "$ws_broker" get stagingworkspaces -o yaml > broker-stagingworkspaces.yaml 2>&1 || true
    kubectl --kubeconfig "$ws_broker" get migrations -o yaml > broker-migrations.yaml 2>&1 || true
    kubectl --kubeconfig "$ws_staging" get workspaces -o yaml > staging-workspaces.yaml 2>&1 || true
    kubectl --kubeconfig "$ws_verification" get workspaces -o yaml > verification-workspaces.yaml 2>&1 || true

    # Stage resources in the compute (platform) cluster.
    kubectl --kubeconfig "$kind_platform" get jobs,secrets -o yaml > compute-stage-resources.yaml 2>&1 || true

    for name in kropg cnpg; do
        kubectl --kubeconfig "$workspace_kubeconfigs/$name.kubeconfig" get acceptapis -o yaml > "$name-acceptapis.yaml" 2>&1 || true
        kubectl --kubeconfig "$kubeconfigs/$name.kubeconfig" logs "deployment/api-syncagent-$name" > "$name-api-syncagent.log" 2>&1 || true
        kubectl --kubeconfig "$kubeconfigs/$name.kubeconfig" get postgres.example.platform-mesh.io -A -o yaml > "$name-postgres.yaml" 2>&1 || true
    done
    kubectl --kubeconfig "$kind_cnpg" logs -n cnpg-system deployment/cnpg-cloudnative-pg > cnpg-operator.log 2>&1 || true
}

case "$1" in
    (setup) _setup ;;
    (cleanup) _cleanup ;;
    (start-broker) _start_broker ;;
    (stop-broker) _stop_broker ;;
    (ci) _ci;;
    (*) die "Unknown command: $1" ;;
esac
