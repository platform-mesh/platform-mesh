# Copyright The Platform Mesh Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

timeout="10m"

log() { echo ">>> $@"; }
die() { echo "!!! $@" >&2; exit 1; }

kind::cluster() {
    local name="broker-$1"
    local kubeconfig="$2"
    rm -f "$kubeconfig"
    if ! kind get clusters | grep -q "^$name$"; then
        kind create cluster --name "$name" --kubeconfig "$kubeconfig" \
            || die "Failed to create cluster $name"
    else
        kind export kubeconfig --name "$name" --kubeconfig "$kubeconfig" \
            || die "Failed to export kubeconfig for cluster $name"
    fi
}

kind::load::image() {
    local name="broker-$1"
    local image="$2"
    if ! docker image inspect "$image" >/dev/null 2>&1; then
        if ! docker pull "$image"; then
            log "Failed to pull image $image, skipping pre-load"
            return 0
        fi
    fi
    kind load docker-image --name "$name" "$image" \
        || log "Failed to load image $image into cluster $name"
}

kubectl::apply::one() {
    local kubeconfig="$1"
    local resource="$2"
    local try_count=0
    local max_retries=30

    while [[ "$try_count" -lt "$max_retries" ]]; do
        if kubectl --kubeconfig "$kubeconfig" apply -f "$resource"
        then
            return
        else
            try_count=$((try_count + 1))
            log "kubectl apply failed, retrying ($try_count/$max_retries)..."
            sleep 2
        fi
    done

    die "Failed to apply $* to cluster with kubeconfig $kubeconfig after $max_retries attempts"
}

kubectl::apply() {
    local kubeconfig="$1"
    shift 1
    for resource in "$@"; do
        kubectl::apply::one "$kubeconfig" "$resource"
    done
}

kubectl::delete::one() {
    local kubeconfig="$1"
    local resource="$2"

    kubectl --kubeconfig "$kubeconfig" delete "$resource" --ignore-not-found --wait=false
    kubectl --kubeconfig "$kubeconfig" \
        patch "$resource" \
        --type=json \
        --patch='[{"op":"remove","path":"/metadata/finalizers"}]'
}

kubectl::delete() {
    local kubeconfig="$1"
    shift 1
    for resource in "$@"; do
        kubectl::delete::one "$kubeconfig" "$resource"
    done
}

kubectl::kustomize() {
    local kubeconfig="$1"
    local kustomize_dir="$2"
    local try_count=0
    local max_retries=30

    while [[ "$try_count" -lt "$max_retries" ]]; do
        if kubectl --kubeconfig "$kubeconfig" kustomize --load-restrictor=LoadRestrictionsNone "$kustomize_dir" \
                | kubectl --kubeconfig "$kubeconfig" apply -f-
        then
            return
        else
            try_count=$((try_count + 1))
            log "kustomize apply failed, retrying ($try_count/$max_retries)..."
            sleep 2
        fi
    done

    die "Failed to kustomize apply $kustomize_dir to cluster with kubeconfig $kubeconfig after $max_retries attempts"
}

kubectl::wait() {
    local kubeconfig="$1"
    local resource="$2"
    local namespace="$3"
    local for="$4"

    kubectl --kubeconfig "$kubeconfig" wait --for="$for" "$resource" --timeout="$timeout" --namespace="$namespace" \
        || die "Timed out waiting for $for on $resource in cluster with kubeconfig $kubeconfig"
}

kubectl::wait::_list() {
    local kubeconfig="$1"
    local resource="$2"
    shift 2
    kubectl --kubeconfig "$kubeconfig" get "$resource" "$@" -o json | jq '.items | length'
}

kubectl::wait::list() {
    local _kubeconfig="$1"
    local _resource="$2"
    local retry_count=0
    local max_retries=360
    while [[ "$(kubectl::wait::_list "$@")" -eq 0 ]] ; do
        retry_count=$((retry_count + 1))
        if [[ $retry_count -ge $max_retries ]]; then
            die "Timed out waiting for any resources '$_resource': $_kubeconfig"
        fi
        if [[ $((retry_count % 30)) -eq 0 ]]; then
            log "Still waiting for '$_resource' ($retry_count/$max_retries)... kubeconfig=$_kubeconfig"
            # Print additional debug info every 30 seconds
            kubectl --kubeconfig "$_kubeconfig" get "$_resource" --all-namespaces 2>&1 || true
        fi
        sleep 1
    done
    log "Found '$_resource' after $retry_count retries"
}

kubectl::wait::empty() {
    local _kubeconfig="$1"
    local _resource="$2"
    local retry_count=0
    local max_retries=360
    while [[ "$(kubectl::wait::_list "$@")" -gt 0 ]] ; do
        retry_count=$((retry_count + 1))
        if [[ $retry_count -ge $max_retries ]]; then
            die "Timed out waiting for '$_resource' to be empty: $_kubeconfig"
        fi
        if [[ $((retry_count % 30)) -eq 0 ]]; then
            log "Still waiting for '$_resource' to be empty ($retry_count/$max_retries)... kubeconfig=$_kubeconfig"
            # Print additional debug info every 30 seconds
            kubectl --kubeconfig "$_kubeconfig" get "$_resource" --all-namespaces 2>&1 || true
        fi
        sleep 1
    done
    log "No '$_resource' left after $retry_count retries"
}

kubectl::wait::suffix() {
    local kubeconfig="$1"
    local resource="$2"
    local namespace="$3"
    local jsonpath="$4"
    local suffix="$5"

    kubectl::wait "$kubeconfig" "$resource" "$namespace" create
    kubectl::wait "$kubeconfig" "$resource" "$namespace" jsonpath="{$jsonpath}"

    local value="$(kubectl --kubeconfig "$kubeconfig" get "$resource" -n "$namespace" -o "jsonpath={$jsonpath}")"
    local retry_count=0
    local max_retries=360
    while [[ "$value" != *"$suffix" ]]; do
        log "Current $jsonpath is '$value', waiting for suffix '$suffix'"
        retry_count=$((retry_count + 1))
        if [[ $retry_count -ge $max_retries ]]; then
            die "Timed out waiting for correct suffix in $kubeconfig"
        fi
        sleep 1
        value="$(kubectl --kubeconfig "$kubeconfig" get "$resource" -n "$namespace" -o "jsonpath={$jsonpath}")"
    done
    log "Found expected suffix '$suffix'"
}

kubectl::secret::debase64() {
    local kubeconfig="$1"
    local resource="$2"
    local namespace="$3"
    local jsonpath="$4"

    # redirect to stderr to not pollute the output
    kubectl::wait "$kubeconfig" "secret/$resource" "$namespace" create >&2
    kubectl::wait "$kubeconfig" "secret/$resource" "$namespace" jsonpath="{$jsonpath}" >&2
    kubectl --kubeconfig "$kubeconfig" get secret "$resource" -n "$namespace" -o "jsonpath={$jsonpath}" | base64 -d
}

kubectl::wait::cert::subject() {
    local kubeconfig="$1"
    local resource="$2"
    local namespace="$3"
    local expected_subject="$4"
    shift 4

    local subject="$(kubectl::secret::debase64 "$kubeconfig" "$resource" "$namespace" ".data.tls\.crt" | openssl x509 -noout -subject)"
    local retry_count=0
    local max_retries=360
    while [[ "$subject" != *"$expected_subject"* ]]; do
        log "Current subject is '$subject', waiting for '$expected_subject'"
        retry_count=$((retry_count + 1))
        if [[ $retry_count -ge $max_retries ]]; then
            die "Timed out waiting for correct certificate in $kubeconfig"
        fi
        sleep 1
        subject="$(kubectl::secret::debase64 "$kubeconfig" "$resource" "$namespace" ".data.tls\.crt" | openssl x509 -noout -subject)"
    done
    log "Found expected subject '$expected_subject'"
}

kubectl::wait::not_empty() {
    local kubeconfig="$1"
    local resource="$2"
    local jsonpath="$3"

    local try_count=0
    local max_retries=120
    while [[ "$try_count" -lt "$max_retries" ]]; do
        local value="$(kubectl --kubeconfig "$kubeconfig" get "$resource" -o "jsonpath=$jsonpath")"
        if [[ -n "$value" ]]; then
            return
        else
            try_count=$((try_count + 1))
            log "Value at $jsonpath is empty, retrying ($try_count/$max_retries)..."
            sleep 2
        fi
    done
    die "Failed to get non-empty value at $jsonpath for $resource in cluster with kubeconfig $kubeconfig after $max_retries attempts"
}

helm::repo() {
    local name="$1"
    local url="$2"
    helm repo add "$name" "$url" || die "Failed to add helm repo $name with url $url"
    helm repo update "$name" || die "Failed to update helm repo $name"
}

helm::install() {
    local kubeconfig="$1"
    local release_name="$2"
    local chart_path="$3"
    shift 3

    KUBECONFIG="$kubeconfig" \
        helm upgrade --install \
        --create-namespace \
        "$release_name" \
        "$chart_path" \
        "$@" || die "Failed to install helm chart $chart_path as release $release_name"
}

helm::install::certmanager() {
    local kubeconfig="$1"
    shift 1
    helm::install "$kubeconfig" \
        cert-manager oci://quay.io/jetstack/charts/cert-manager:v1.19.1 \
          --set crds.enabled=true \
          --set enableCertificateOwnerRef=true \
          "$@"
}

helm::install::etcddruid() {
    local kubeconfig="$1"
    shift 1
    local version="v0.33.0"
    kubectl::apply "$kubeconfig" \
        "https://raw.githubusercontent.com/gardener/etcd-druid/refs/tags/${version}/api/core/v1alpha1/crds/druid.gardener.cloud_etcds_without_cel.yaml"
    kubectl::apply "$kubeconfig" \
        "https://raw.githubusercontent.com/gardener/etcd-druid/refs/tags/${version}/api/core/v1alpha1/crds/druid.gardener.cloud_etcdcopybackupstasks.yaml"
    helm::install "$kubeconfig" \
        etcd-druid "oci://europe-docker.pkg.dev/gardener-project/releases/charts/gardener/etcd-druid:${version}" \
        "$@"
}

helm::install::kcp() {
    local kubeconfig="$1"
    shift 1
    helm::repo kcp  https://kcp-dev.github.io/helm-charts
    helm::install "$kubeconfig" \
        kcp-operator kcp/kcp-operator \
        --version=0.4.0 \
        "$@"
}

helm::install::kro() {
    local kubeconfig="$1"
    shift 1
    helm::install "$kubeconfig" \
        kro oci://registry.k8s.io/kro/charts/kro \
        --version=0.5.1 \
        "$@"
}

helm::install::cnpg() {
    local kubeconfig="$1"
    shift 1
    helm::repo cnpg https://cloudnative-pg.github.io/charts
    helm::install "$kubeconfig" \
        cnpg cnpg/cloudnative-pg \
        --version=0.29.0 \
        --namespace=cnpg-system \
        --create-namespace \
        "$@"
}

helm::install::api_syncagent() {
    local kubeconfig="$1"
    local apiExportName="$2"
    local agentName="$3"
    local kcpKubeconfig="$4"
    shift 4

    if [[ -z "$kubeconfig" || -z "$apiExportName" || -z "$agentName" || -z "$kcpKubeconfig" ]]; then
        die "kubeconfig, apiExportName, agentName, and kcpKubeconfig are required"
    fi

    helm::repo kcp  https://kcp-dev.github.io/helm-charts
    helm::install "$kubeconfig" "api-syncagent-$agentName" kcp/api-syncagent \
        --version=0.4.5 \
        --set replicas=1 \
        --set apiExportName="$apiExportName" \
        --set agentName="$agentName" \
        --set kcpKubeconfig="$kcpKubeconfig" \
        --set "hostAliases.values[0].ip=10.96.188.4" \
        --set "hostAliases.values[0].hostnames[0]=localhost" \
        --set "hostAliases.values[0].hostnames[1]=root.kcp.localhost" \
        "$@"
}

apisyncagent::publish() {
    local kubeconfig="$1"
    local resource="$2"
    local kind="$3"
    local group="$4"
    local versions="$5"
    shift 5
    if [[ -z "$resource" || -z "$kind" || -z "$group" || -z "$versions" ]]; then
        die "resource, kind, group, and versions are required"
    fi

    local name="$resource.$group"
    local suffix=""
    [[ -n "$AGENT_NAME" ]] && suffix="-$AGENT_NAME"

    {
        echo "apiVersion: syncagent.kcp.io/v1alpha1"
        echo "kind: PublishedResource"
        echo "metadata:"
        echo "  name: $name"
        echo "spec:"
        echo "  resource:"
        echo "    kind: $kind"
        echo "    apiGroup: $group"
        echo "    versions: [$versions]"
        echo "  related:"
        while [[ "$#" -gt 0 ]]; do
            apisyncagent::publish::related "$@"
            shift 4
        done
        echo "---"
        echo "apiVersion: rbac.authorization.k8s.io/v1"
        echo "kind: ClusterRole"
        echo "metadata:"
        echo "  name: api-syncagent$suffix:$resource"
        echo "rules:"
        echo "  - apiGroups:"
        echo "      - $group"
        echo "    resources:"
        echo "      - $resource"
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
        echo "  name: api-syncagent$suffix:$resource"
        echo "roleRef:"
        echo "  apiGroup: rbac.authorization.k8s.io"
        echo "  kind: ClusterRole"
        echo "  name: api-syncagent$suffix:$resource"
        echo "subjects:"
        echo "  - kind: ServiceAccount"
        echo "    name: api-syncagent$suffix"
        echo "    namespace: ${NAMESPACE:-default}"
    } | kubectl::apply "$kubeconfig" -
}

apisyncagent::publish::related() {
    local identifier="$1"
    local origin="$2"
    local kind="$3"
    local path="$4"
    if [[ -z "$identifier" || -z "$origin" || -z "$kind" || -z "$path" ]]; then
        die "identifier, origin, kind, and path are required for related resource"
    fi

    echo "  - identifier: $identifier"
    echo "    origin: $origin"
    echo "    kind: $kind"
    echo "    object:"
    echo "      reference:"
    echo "        path: $path"
}

kubeconfig::hostname() {
    local kubeconfig="$1"
    local hostname="$(yq '.clusters[0].cluster.server' "$kubeconfig")"
    [[ -z "$hostname" ]] && die "Failed to get server from kubeconfig $kubeconfig"
    hostname="${hostname#http://}"
    hostname="${hostname#https://}"
    echo "${hostname%%/*}"
}

kubeconfig::hostname::set() {
    local kubeconfig="$1"
    local old_hostname="$2"
    local new_hostname="$3"
    yq -i ".clusters[].cluster.server |= sub(\"$old_hostname\"; \"$new_hostname\")" "$kubeconfig"
}

kubectl::kubeconfig::secret() {
    local kubeconfig="$1"
    local target="$2"
    local name="$3"
    local namespace="$4"
    local hostname="$5"

    cp "$target" "$target.tmp"
    target="$target.tmp"

    if [[ -n "$hostname" ]]; then
        local cur_hostname="$(kubeconfig::hostname "$target")"
        kubeconfig::hostname::set "$target" "$cur_hostname" "$hostname"
    fi

    kubectl create secret generic "kubeconfig-$name" --namespace="$namespace" --dry-run=client -o yaml \
        --from-file=kubeconfig="$target" \
        | kubectl::apply "$kubeconfig" "-"
    rm -f "$target"
}

kubectl::kubeconfig::current_server_url() {
    local kubeconfig="$1"
    local current_context="$(kubectl --kubeconfig "$kubeconfig" config current-context)"
    kubectl --kubeconfig "$kubeconfig" config view -o jsonpath="{.clusters[?(@.name==\"$current_context\")].cluster.server}"
}

docker::local_port() {
    local container_name="$1"
    local container_port="$2"
    docker port "$container_name" "$container_port" | cut -d' ' -f3
}

kubectl::krew::setup() {
    if kubectl krew version &>/dev/null; then
        return
    fi
    # verbatim from https://krew.sigs.k8s.io/docs/user-guide/setup/install/
    (
      set -x; cd "$(mktemp -d)" &&
      OS="$(uname | tr '[:upper:]' '[:lower:]')" &&
      ARCH="$(uname -m | sed -e 's/x86_64/amd64/' -e 's/\(arm\)\(64\)\?.*/\1\2/' -e 's/aarch64$/arm64/')" &&
      KREW="krew-${OS}_${ARCH}" &&
      curl -fsSLO "https://github.com/kubernetes-sigs/krew/releases/latest/download/${KREW}.tar.gz" &&
      tar zxvf "${KREW}.tar.gz" &&
      ./"${KREW}" install krew
    ) \
        || die "Failed to install krew"
    export PATH="${KREW_ROOT:-$HOME/.krew}/bin:$PATH"
}

kcp::setup::plugins() {
    kubectl::krew::setup
    if ! kubectl krew index list | grep -q "^kcp-dev"; then
        kubectl krew index add kcp-dev https://github.com/kcp-dev/krew-index.git \
            || die "Failed to add kcp-dev krew index"
    fi
    kubectl krew install kcp-dev/kcp \
        || die "Failed to install kcp krew plugin"
    kubectl krew install kcp-dev/ws \
        || die "Failed to install ws krew plugin"
    kubectl krew install kcp-dev/create-workspace \
        || die "Failed to install create-workspace krew plugin"
}

kcp::setup::kubeconfigs() {
    local kind_kubeconfig="$1"
    local kcp_kubeconfig="$2"
    local kcp_host_kubeconfig="$3"

    KUBECONFIG="$kind_kubeconfig" \
        kubectl wait --for=create secret/admin-kubeconfig \
            --timeout="$timeout" \
            || die "Timed out waiting for admin-kubeconfig secret in kind cluster"

    KUBECONFIG="$kind_kubeconfig" \
        kubectl get secret admin-kubeconfig -o jsonpath='{.data.kubeconfig}' \
        | base64 -d \
        > "$kcp_kubeconfig" \
        || die "Failed to get admin kubeconfig from kind cluster"

    # Replace the port with the port-forwarded port

    # Create port forward to access kcp from host
    kcp::front_proxy_forward "$kind_kubeconfig" "8443"

    cp "$kcp_kubeconfig" "$kcp_host_kubeconfig"
    yq -i ".clusters[].cluster.server |= sub(\":32443\"; \":8443\")" "$kcp_host_kubeconfig"
    local hostname="$(kubectl --kubeconfig "$kind_kubeconfig" get rootshards.operator.kcp.io root -o jsonpath='{.spec.external.hostname}')"
    kubeconfig::hostname::set "$kcp_host_kubeconfig" "$hostname:32443" "127.0.0.1:8443"
}

kcp::front_proxy_forward() {
    local kubeconfig="$1"
    local port="$2"
    KUBECONFIG="$kubeconfig" \
        kubectl wait --for=condition=Available=True deployment/frontproxy-front-proxy \
            --timeout="$timeout" \
            || die "front proxy is not available"
    KUBECONFIG="$kubeconfig" \
        kubectl port-forward svc/frontproxy-front-proxy "$port:6443" 2>/dev/null >/dev/null &
}

kcp::create_workspace() {
    local parent_kubeconfig="$1"
    [[ -z "$parent_kubeconfig" ]] && die "parent_kubeconfig is required"
    local target_kubeconfig="$2"
    [[ -z "$target_kubeconfig" ]] && die "target_kubeconfig is required"
    local wsname="$3"
    [[ -z "$wsname" ]] && die "wsname is required"

    local current_server="$(kubeconfig::hostname "$parent_kubeconfig")"
    local local_server="127.0.0.1:8443"

    cp "$parent_kubeconfig" "$target_kubeconfig" \
        || die "Failed to copy kubeconfig from $parent_kubeconfig to $target_kubeconfig"
    kubeconfig::hostname::set "$target_kubeconfig" "$current_server" "$local_server"
    local check_kubeconfig="$target_kubeconfig.check"
    cp "$target_kubeconfig" "$check_kubeconfig"

    # Wait until the parent workspace serves the tenancy API. WorkspaceTypes
    # only exist in root, so they cannot be used as a readiness gate for
    # nested workspaces.
    while ! KUBECONFIG="$target_kubeconfig" kubectl get workspaces &>/dev/null; do
        log "Workspace API not ready yet, retrying..."
        sleep 2
    done

    log "Creating workspace $wsname"
    local attempt created=""
    for attempt in {1..30}; do
        if KUBECONFIG="$target_kubeconfig" \
            kubectl create-workspace "$wsname" --enter --ignore-existing; then
            created=1
            break
        fi
        # Right after kcp bootstrap the workspace type may not be ready yet.
        log "Failed to create workspace $wsname (attempt $attempt), retrying..."
        sleep 2
    done
    [[ -n "$created" ]] || die "Failed to create workspace $wsname"

    log "Waiting for workspace $wsname to become Ready"
    while ! KUBECONFIG="$check_kubeconfig" kubectl get workspace "$wsname" &>/dev/null; do
        log "Workspace $wsname not found yet, retrying..."
        sleep 2
    done
    KUBECONFIG="$check_kubeconfig" \
        kubectl wait --for=jsonpath='{.status.phase}="Ready"' \
            workspace "$wsname" --timeout="$timeout" \
            || die "Timed out waiting for workspace $wsname to become Ready"
    rm -f "$check_kubeconfig"
}

kcp::apiexport() {
    local kubeconfig="$1"
    local crd_file="$2"
    shift 2

    # Strip leading comment-only documents (license headers); kcp crd
    # snapshot fails on documents without a kind.
    local stripped_crd
    stripped_crd="$(mktemp)"
    yq 'select(.kind == "CustomResourceDefinition") | ... comments=""' "$crd_file" >"$stripped_crd" \
        || die "Failed to extract CRD from $crd_file"

    local schema
    schema="$(kubectl kcp crd snapshot --filename "$stripped_crd" --prefix current)" \
        || die "Failed to snapshot CRD $crd_file"
    KUBECONFIG="$kubeconfig" kubectl apply -f- <<<"$schema" \
        || die "Failed to apply APIResourceSchema for $crd_file"

    local group="$(yq '.spec.group' "$stripped_crd")"
    local export_name="$(yq '.spec.names.plural' "$stripped_crd")"
    rm -f "$stripped_crd"

    {
        echo "apiVersion: apis.kcp.io/v1alpha2"
        echo "kind: APIExport"
        echo "metadata:"
        echo "  name: $export_name"
        echo "spec:"
        echo "  resources:"
        echo "    - group: $group"
        echo "      name: $export_name"
        echo "      schema: current.${export_name}.${group}"
        if [[ "$#" -gt 0 ]]; then
            echo "  permissionClaims:"
        fi
        while [[ "$#" -gt 0 ]]; do
            local resource="$1"
            local verbs="$2"
            shift 2
            [[ -z "$resource" ]] && die "resource name is required for permissionClaims"
            [[ -z "$verbs" ]] && die "verbs are required for resource $resource"
            local group="" # TODO split resource into group/resource if needed
            echo "    - resource: $resource"
            echo "      group: '$group'"
            echo "      verbs:"
            if [[ "$verbs" == "*" ]]; then
                echo "        - '*'"
                continue
            fi
            for verb in ${verbs//,/ }; do
                echo "        - '$verb'"
            done
        done
    } | KUBECONFIG="$kubeconfig" kubectl apply -f- \
        || die "Failed to create apiexport $export_name"

    KUBECONFIG="$kubeconfig" \
        kubectl wait --for=condition=IdentityValid=True apiexports "$export_name" --timeout="$timeout" \
            || die "Timed out waiting for apiexport $export_name to become valid"
}

kcp::apibinding() {
    local kubeconfig="$1"
    local export_ws="$2"
    local export_name="$3"
    shift 3

    {
        echo "apiVersion: apis.kcp.io/v1alpha2"
        echo "kind: APIBinding"
        echo "metadata:"
        echo "  name: $export_name"
        echo "spec:"
        echo "  reference:"
        echo "    export:"
        echo "      path: ${export_ws}"
        echo "      name: ${export_name}"
        if [[ "$#" -gt 0 ]]; then
            echo "  permissionClaims:"
        fi
        while [[ "$#" -gt 0 ]]; do
            local resource="$1"
            local group="$2"
            local verbs="$3"
            shift 3
            [[ -z "$resource" ]] && die "resource name is required for permissionClaims"
            [[ -z "$verbs" ]] && die "verbs are required for resource $resource"
            echo "    - resource: $resource"
            echo "      group: '$group'"
            echo "      state: Accepted"
            echo "      selector:"
            echo "        matchAll: true"
            echo "      verbs:"
            # special handling for wildcard because shell expansion
            if [[ "$verbs" == "*" ]]; then
                echo "        - '*'"
                continue
            fi
            for verb in ${verbs//,/ }; do
                echo "        - '$verb'"
            done
            # echo "          - key: metadata.name"
            # echo "            operator: In"
            # echo "            values:"
            # echo "              - '$name'"
        done
    } | KUBECONFIG="$kubeconfig" kubectl apply -f- \
        || die "Failed to create apibinding $export_name from $export_ws"

    KUBECONFIG="$kubeconfig" \
        kubectl wait --for=condition=Ready=True apibindings "$export_name" --timeout="$timeout" \
            || die "Timed out waiting for apibinding $export_name to become ready"
}

kcp::serviceaccount::admin() {
    local kubeconfig="$1"
    local sa_name="$2"
    local namespace="$3"
    [[ -z "$namespace" ]] && namespace="default"

    KUBECONFIG="$kubeconfig" \
        kubectl create serviceaccount "$sa_name" -n "$namespace" --dry-run=client -o yaml \
            | KUBECONFIG="$kubeconfig" kubectl apply -f- >/dev/null \
            || die "Failed to create service account $sa_name in namespace $namespace"

    KUBECONFIG="$kubeconfig" \
        kubectl create clusterrolebinding "$sa_name" -n "$namespace" --dry-run=client -o yaml \
            --clusterrole=cluster-admin \
            --serviceaccount="${namespace}:${sa_name}" \
            | KUBECONFIG="$kubeconfig" kubectl apply -f- >/dev/null \
            || die "Failed to create clusterrolebinding for service account $sa_name in namespace $namespace"

    KUBECONFIG="$kubeconfig" kubectl create token "$sa_name" --namespace "$namespace" --duration=5208h \
        || die "Failed to create token for service account $sa_name in namespace $namespace"
}

kubectl::serviceaccount::kubeconfig() {
    local target_kubeconfig="$1"
    local sa_name="$2"
    local namespace="$3"
    local output_kubeconfig="$4"
    local hostname="$5"
    [[ -z "$namespace" ]] && namespace="default"

    local token="$(kubectl --kubeconfig "$target_kubeconfig" create token "$sa_name" --namespace "$namespace" --duration=5208h)" \
        || die "Failed to create token for service account $sa_name in namespace $namespace"

    local url="$(kubectl::kubeconfig::current_server_url "$target_kubeconfig")"
    if [[ -n "$hostname" ]]; then
        # Replace the hostname in the URL for in-cluster access
        local scheme="${url%%://*}"
        url="${scheme}://${hostname}"
    fi

    local ca_data="$(kubectl --kubeconfig "$target_kubeconfig" config view --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')"

    kubeconfig::create::bare "$output_kubeconfig"
    if [[ -n "$ca_data" ]]; then
        KUBECONFIG="$output_kubeconfig" \
            kubectl config set-cluster default --server="$url" \
            || die "Failed to set cluster in kubeconfig $output_kubeconfig"
        # Set CA data directly via yq since kubectl doesn't have a flag for it
        yq -i ".clusters[0].cluster.certificate-authority-data = \"$ca_data\"" "$output_kubeconfig"
    else
        KUBECONFIG="$output_kubeconfig" \
            kubectl config set-cluster default --insecure-skip-tls-verify=true --server="$url" \
            || die "Failed to set cluster in kubeconfig $output_kubeconfig"
    fi
    KUBECONFIG="$output_kubeconfig" \
        kubectl config set-credentials default --token="$token" \
        || die "Failed to set user credentials in kubeconfig $output_kubeconfig"
}

kubeconfig::create::bare() {
    local kubeconfig="$1"

    echo "" > "$kubeconfig"
    KUBECONFIG="$kubeconfig" \
        kubectl config set-context default --cluster=default --user=default \
        || die "Failed to set context in kubeconfig $kubeconfig"
    KUBECONFIG="$kubeconfig" \
        kubectl config use-context default \
        || die "Failed to use context in kubeconfig $kubeconfig"
}

kubeconfig::create::token() {
    local kubeconfig="$1"
    local url="$2"
    local token="$3"

    kubeconfig::create::bare "$kubeconfig"
    # TODO: Include TLS certs, could pull them from other kubeconfigs
    KUBECONFIG="$kubeconfig" \
        kubectl config set-cluster default --insecure-skip-tls-verify=true --server="$url" \
        || die "Failed to set cluster in kubeconfig $kubeconfig"
    KUBECONFIG="$kubeconfig" \
        kubectl config set-credentials default --token="$token" \
        || die "Failed to set user credentials in kubeconfig $kubeconfig"
}
