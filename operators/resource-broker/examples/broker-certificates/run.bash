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

kind_internalca="$kubeconfigs/internalca.kubeconfig"
ws_internalca="$workspace_kubeconfigs/internalca.kubeconfig"

kind_externalca="$kubeconfigs/externalca.kubeconfig"
ws_externalca="$workspace_kubeconfigs/externalca.kubeconfig"

kind_consumer="$kubeconfigs/consumer.kubeconfig"
ws_consumer="$workspace_kubeconfigs/consumer.kubeconfig"

_setup() {
    log "Setting up platform cluster"
    kind::cluster platform "$kind_platform"
    helm::install::certmanager "$kind_platform"
    helm::install::etcddruid "$kind_platform"
    helm::install::kcp "$kind_platform"

    log "Setting up kcp"
    kubectl::kustomize "$kind_platform" "./examples/broker-certificates/platform"
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

    log "Setting up Certificate APIExport for consumers"
    kcp::apiexport "$ws_platform" ./config/example/crd/example.platform-mesh.io_certificates.yaml \
        secrets '*' \
        events '*' \
        namespaces '*'

    log "Setting up internalca kcp workspace"
    kcp::create_workspace "$kubeconfigs/kcp-admin.kubeconfig" "$ws_internalca" "internalca"
    _provider_setup_new internalca "$kind_internalca" "$ws_internalca" internal.corp

    log "Setting up externalca kcp workspace"
    kcp::create_workspace "$kubeconfigs/kcp-admin.kubeconfig" "$ws_externalca" "externalca"
    _provider_setup_new externalca "$kind_externalca" "$ws_externalca" corp.com

    # log "Setting up consumer kind cluster"
    # TODO setup with kube-bind
    # kind::cluster consumer "$kind_consumer"

    log "Setting up consumer kcp workspace"
    kcp::create_workspace "$kubeconfigs/kcp-admin.kubeconfig" "$ws_consumer" "consumer"

}

_provider_setup_new() {
    local name="$1"
    local kind_kubeconfig="$2"
    local ws_kubeconfig="$3"
    local suffix="$4"

    kcp::create_workspace "$kubeconfigs/kcp-admin.kubeconfig" "$ws_kubeconfig" "$name"

    log "Creating APIExport certificates in $name workspace"
    {
        echo "apiVersion: apis.kcp.io/v1alpha1"
        echo "kind: APIExport"
        echo "metadata:"
        echo "  name: certificates"
    } | kubectl::apply "$ws_kubeconfig" "-"

    log "Setting up $name kind cluster"
    kind::cluster "$name" "$kind_kubeconfig"
    helm::install::kro "$kind_kubeconfig"
    helm::install::certmanager "$kind_kubeconfig"
    # Installing the same resources as in the non-kcp example
    kubectl::kustomize "$kind_kubeconfig" "$example_dir/$name"
    kubectl::wait "$kind_kubeconfig" rgd/certificates.example.platform-mesh.io "" create
    kubectl::wait "$kind_kubeconfig" rgd/certificates.example.platform-mesh.io "" condition=Ready

    log "Setting up api-syncagent in $name kind cluster"

    local api_syncagent_token="$(kcp::serviceaccount::admin "$ws_kubeconfig" api-syncagent default)"
    local api_syncagent_kubeconfig="$kubeconfigs/api-syncagent-$name.kubeconfig"
    kubeconfig::create::token "$api_syncagent_kubeconfig" \
        "$(kubectl::kubeconfig::current_server_url "$ws_kubeconfig")" \
        "$api_syncagent_token"
    kubectl::kubeconfig::secret "$kind_kubeconfig" "$api_syncagent_kubeconfig" "$name" default "broker-platform-control-plane:32111"

    helm::install::api_syncagent "$kind_kubeconfig" "certificates" "$name" "kubeconfig-$name" \
        --set replicas=1
    AGENT_NAME="$name" apisyncagent::publish "$kind_kubeconfig" \
        "certificates" "Certificate" "example.platform-mesh.io" "v1alpha1" \
        "certificate" "service" "Secret" "status.relatedResources.secret.name"
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

    kubectl::kustomize "$kind_platform" "./examples/broker-certificates/platform/broker"

    kubectl create secret generic kcp-kubeconfig --namespace=resource-broker-system --dry-run=client -o yaml \
        --from-file=kubeconfig="$kubeconfigs/operator.kubeconfig" \
        | kubectl::apply "$kind_platform" "-"

    kubectl::wait "$kind_platform" deployment/resource-broker resource-broker-system condition=Available
}

_stop_broker() {
    kubectl --kubeconfig "$kind_platform" delete -k ./examples/broker-certificates/platform/broker
}

_cleanup() {
    log "Cleaning up example resources in consumer ws"
    # The broker cascades the deletion from here: the staging copy, related
    # resources, the Assignment and the StagingWorkspace are all cleaned up
    # by their finalizers.
    kubectl::delete "$ws_consumer" certificates.example.platform-mesh.io/cert-from-consumer

    log "Cleaning up AcceptAPIs in provider workspaces"
    kubectl --kubeconfig "$ws_internalca" delete --ignore-not-found acceptapis --all
    kubectl --kubeconfig "$ws_externalca" delete --ignore-not-found acceptapis --all

    # api-syncagent creates its own names and namespaces on the compute
    # clusters, so query them for leftovers.
    local provider_cert provider_ns

    log "Cleaning up example resources in internalca"
    provider_cert="$(kubectl --kubeconfig "$kind_internalca" get certificates.example.platform-mesh.io -A -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)" || true
    provider_ns="$(kubectl --kubeconfig "$kind_internalca" get certificates.example.platform-mesh.io -A -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null)" || true
    if [[ -n "$provider_cert" ]]; then
        kubectl --kubeconfig "$kind_internalca" delete --ignore-not-found -n "$provider_ns" \
            "certificates.example.platform-mesh.io/$provider_cert"
        kubectl --kubeconfig "$kind_internalca" delete --ignore-not-found -n "$provider_ns" \
            --selector kro.run/owned=true secrets
    fi

    log "Cleaning up example resources in externalca"
    provider_cert="$(kubectl --kubeconfig "$kind_externalca" get certificates.example.platform-mesh.io -A -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)" || true
    provider_ns="$(kubectl --kubeconfig "$kind_externalca" get certificates.example.platform-mesh.io -A -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null)" || true
    if [[ -n "$provider_cert" ]]; then
        kubectl --kubeconfig "$kind_externalca" delete --ignore-not-found -n "$provider_ns" \
            "certificates.example.platform-mesh.io/$provider_cert"
        kubectl --kubeconfig "$kind_externalca" delete --ignore-not-found -n "$provider_ns" \
            --selector kro.run/owned=true secrets
    fi

    log "Cleaning up Certificate APIBindings and APIExports"
    kubectl --kubeconfig "$ws_consumer" delete apibinding certificates
    kubectl --kubeconfig "$ws_internalca" delete apibinding certificates
    kubectl --kubeconfig "$ws_internalca" delete apiexport certificates
    kubectl --kubeconfig "$ws_externalca" delete apibinding certificates
    kubectl --kubeconfig "$ws_externalca" delete apiexport certificates

    log "Cleaning up AcceptAPI APIBindings"
    kubectl --kubeconfig "$ws_internalca" delete apibinding acceptapis
    kubectl --kubeconfig "$ws_externalca" delete apibinding acceptapis

    return 0
}

_ci() {
    # Collect logs for debugging - ignore errors since resources may not exist if test failed early
    kubectl --kubeconfig "$kind_platform" logs -n resource-broker-system deployment/resource-broker > resource-broker.log 2>&1 || true
    kubectl --kubeconfig "$ws_consumer" get certificates.example.platform-mesh.io cert-from-consumer -o yaml > consumer-certificate.yaml 2>&1 || true

    # Broker-internal state: Assignments and StagingWorkspaces in the
    # broker (coordination) workspace, verification/staging workspaces
    # under their tree roots.
    kubectl --kubeconfig "$ws_broker" get assignments -o yaml > broker-assignments.yaml 2>&1 || true
    kubectl --kubeconfig "$ws_broker" get stagingworkspaces -o yaml > broker-stagingworkspaces.yaml 2>&1 || true
    kubectl --kubeconfig "$ws_staging" get workspaces -o yaml > staging-workspaces.yaml 2>&1 || true
    kubectl --kubeconfig "$ws_verification" get workspaces -o yaml > verification-workspaces.yaml 2>&1 || true

    kubectl --kubeconfig "$ws_internalca" get acceptapis -o yaml > internalca-acceptapis.yaml 2>&1 || true
    kubectl --kubeconfig "$kind_internalca" logs deployment/api-syncagent-internalca > internalca-api-syncagent.log 2>&1 || true
    kubectl --kubeconfig "$kind_internalca" logs -n cert-manager deployment/cert-manager > internalca-cert-manager.log 2>&1 || true
    kubectl --kubeconfig "$kind_internalca" get certificates.example.platform-mesh.io -A -o yaml > internalca-certificates.yaml 2>&1 || true

    kubectl --kubeconfig "$ws_externalca" get acceptapis -o yaml > externalca-acceptapis.yaml 2>&1 || true
    kubectl --kubeconfig "$kind_externalca" logs deployment/api-syncagent-externalca > externalca-api-syncagent.log 2>&1 || true
    kubectl --kubeconfig "$kind_externalca" logs -n cert-manager deployment/cert-manager > externalca-cert-manager.log 2>&1 || true
    kubectl --kubeconfig "$kind_externalca" get certificates.example.platform-mesh.io -A -o yaml > externalca-certificates.yaml 2>&1 || true
}

case "$1" in
    (setup) _setup ;;
    (cleanup) _cleanup ;;
    (start-broker) _start_broker ;;
    (stop-broker) _stop_broker ;;
    (ci) _ci;;
    (*) die "Unknown command: $1" ;;
esac
