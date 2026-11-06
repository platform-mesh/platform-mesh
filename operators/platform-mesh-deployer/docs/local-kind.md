# Installing the deployer into a kind cluster

The single-cluster environment [`tests/e2e/deployer`](../../../tests/e2e/deployer)'s `TestSingleCluster`
asserts on, stood up with nothing but `kubectl`, `helm` and `kind`. One
cluster acts as its own config plane and workload cluster.

Use this for a long-lived scratch cluster, a CI-like reproduction, or to see
exactly which objects the environment consists of.

Every command below runs from the repository root, and every `kubectl` is
explicit about `--context` so a stray current-context cannot redirect the
install at another cluster.

## Prerequisites

`kind`, `kubectl`, `helm`, `docker`, and a Go toolchain for the image build.

## 0. Cluster and variables

```bash
kind create cluster --name pm-manual

export CTX=kind-pm-manual
export NS=platform-mesh-system
export PM=dev
export PORT=31443
export IMG=ghcr.io/platform-mesh/platform-mesh/platform-mesh-deployer:dev

# Every hostname in this environment is <name>.$CLUSTER_ID.sslip.io — the node's
# dashed InternalIP, so sslip.io resolves each name back to the node.
export CLUSTER_ID=$(kubectl --context $CTX get nodes \
  -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' | tr '.' '-')
echo "$CLUSTER_ID"

kubectl --context $CTX create namespace $NS
```

`$PORT` must be the **NodePort** (31443, pinned in
[`config/bases/envoy-gateway/gateway.yaml`](../config/bases/envoy-gateway/gateway.yaml)),
not the gateway's own listener port 8443: sslip.io resolves to the node, and
nothing listens on `node:8443`. A shard that advertises a port the node does not
serve comes up healthy and is unreachable.

## 1. CoreDNS

kind's CoreDNS forwards to the host resolver, and many resolvers drop answers
pointing at private IPs (DNS rebind protection) — exactly what sslip.io returns
here. Without this, in-cluster lookups of `<name>.<dashed-ip>.sslip.io` fail and
every shard-to-shard and shard-to-etcd dial fails with `no such host`.

```bash
tmp=$(mktemp -d)
cat > "$tmp/Corefile" <<'EOF'
sslip.io:53 {
    errors
    template IN A sslip.io {
        match "[.-](?P<a>[0-9]{1,3})-(?P<b>[0-9]{1,3})-(?P<c>[0-9]{1,3})-(?P<d>[0-9]{1,3})[.]sslip[.]io[.]$"
        answer "{{ .Name }} 60 IN A {{ .Group.a }}.{{ .Group.b }}.{{ .Group.c }}.{{ .Group.d }}"
        fallthrough
    }
}
EOF
kubectl --context $CTX -n kube-system get configmap coredns -o jsonpath='{.data.Corefile}' >> "$tmp/Corefile"
kubectl --context $CTX -n kube-system create configmap coredns \
  --from-file=Corefile="$tmp/Corefile" --dry-run=client -o yaml | kubectl --context $CTX apply -f -
kubectl --context $CTX -n kube-system rollout restart deployment/coredns
kubectl --context $CTX -n kube-system rollout status deployment/coredns --timeout=180s
rm -rf "$tmp"
```

## 2. cert-manager and the `kcp` ClusterIssuer

```bash
cd operators/platform-mesh-deployer

kubectl --context $CTX apply -k config/bases/cert-manager --server-side --force-conflicts
for d in cert-manager cert-manager-cainjector cert-manager-webhook; do
  kubectl --context $CTX -n cert-manager rollout status deployment/$d --timeout=300s
done

# Retry: the webhook refuses the ClusterIssuer for a few seconds after rollout.
until kubectl --context $CTX apply -k config/bases/cert-issuer --server-side --force-conflicts; do sleep 5; done
```

`kcp` is the self-signed ClusterIssuer the RootShardTemplate's
`certificates.issuerRef` points at.

## 3. Envoy Gateway

```bash
helm --kube-context $CTX upgrade --install envoy \
  oci://registry-1.docker.io/envoyproxy/gateway-helm --version v1.7.0 \
  --namespace envoy-gateway-system --create-namespace --wait --timeout 5m

kubectl --context $CTX apply -k config/bases/envoy-gateway --server-side --force-conflicts
```

This is the environment's only gateway. Its `passthrough` listener has no
`hostnames` restriction, which is what lets one listener carry `fp.…`, `etcd.…`
and the shard hostnames at once — a listener and a route whose hostnames do not
intersect simply never attach, with no error on either object.

## 4. etcd

`config/bases/etcd-tls` carries no namespace of its own, so pass `-n`. The
server certificate and the TLSRoute embed `$CLUSTER_ID` and are therefore
generated here rather than checked in — the same two objects `tests/e2e/deployer`
templates at runtime.

```bash
kubectl --context $CTX apply -k config/bases/etcd-tls -n $NS --server-side --force-conflicts

kubectl --context $CTX apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: etcd-server
  namespace: $NS
spec:
  secretName: etcd-server-tls
  dnsNames:
    - etcd
    - etcd.$NS.svc
    - etcd.$CLUSTER_ID.sslip.io
  usages:
    - server auth
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: etcd-ca
    kind: Issuer
    group: cert-manager.io
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TLSRoute
metadata:
  name: etcd
  namespace: $NS
spec:
  parentRefs:
    - name: eg
      namespace: envoy-gateway-system
      sectionName: passthrough
  hostnames:
    - etcd.$CLUSTER_ID.sslip.io
  rules:
    - backendRefs:
        - name: etcd
          port: 2379
EOF

kubectl --context $CTX -n $NS rollout status deployment/etcd --timeout=300s
```

etcd is reached at `etcd.$CLUSTER_ID.sslip.io` rather than only at `etcd.$NS.svc`
because a shard scheduled onto another cluster resolves the advertised endpoint
verbatim, where a `.svc` name means nothing.

## 5. kcp-operator

The **ntnn fork**, not upstream: the deployer emits admin CRs and expects
something to compile them into `Compiled*` CRs, which upstream has neither the
split controllers nor the CRDs for. It is pinned by the `replace` in
[`go.mod`](../go.mod) — bump the two together.

```bash
kubectl --context $CTX apply -k config/bases/kcp-operator/default --server-side --force-conflicts
kubectl --context $CTX -n kcp-operator-system rollout status \
  deployment/kcp-operator-controller-manager --timeout=300s
```

## 6. The deployer

`ghcr.io/platform-mesh/platform-mesh/platform-mesh-deployer` is not publicly
pullable, so build the image and load it into the node. The build context is the
repository root (the operator resolves `./apis` through the root `go.work`).

```bash
cd ../..
docker build -f operators/platform-mesh-deployer/Dockerfile -t $IMG .
kind load docker-image $IMG --name pm-manual
cd operators/platform-mesh-deployer
```

`config/default` is the real install — CRDs, RBAC and the manager:

```bash
kubectl --context $CTX apply -k config/default --server-side --force-conflicts

# The manifest ships image tag `latest`, which Kubernetes defaults to
# imagePullPolicy: Always — so a locally loaded image is ignored and the pod sits
# in ImagePullBackOff. Patch both fields together, after the apply.
kubectl --context $CTX -n $NS patch deployment platform-mesh-deployer-controller-manager \
  --type=json -p "[
    {\"op\":\"replace\",\"path\":\"/spec/template/spec/containers/0/image\",\"value\":\"$IMG\"},
    {\"op\":\"replace\",\"path\":\"/spec/template/spec/containers/0/imagePullPolicy\",\"value\":\"IfNotPresent\"}
  ]"

kubectl --context $CTX -n $NS rollout status \
  deployment/platform-mesh-deployer-controller-manager --timeout=300s
```

Re-running `apply -k config/default` resets the image, so repeat the patch after
every apply.

## 7. Engage the cluster as its own workload cluster

The deployer reaches every workload cluster through a kubeconfig Secret — there
is no "and also myself" special case. The Secret's **name is load-bearing**:
everything before `--` is the PlatformMesh it belongs to, everything after is the
cluster ID bound to `cluster` in the CEL templates. The label **keys** are the
selector; the values are ignored.

```bash
SA=platform-mesh-deployer-workload

kubectl --context $CTX -n $NS create serviceaccount $SA --dry-run=client -o yaml \
  | kubectl --context $CTX apply -f -
kubectl --context $CTX create clusterrolebinding $SA \
  --clusterrole=cluster-admin --serviceaccount=$NS:$SA --dry-run=client -o yaml \
  | kubectl --context $CTX apply -f -

TOKEN=$(kubectl --context $CTX -n $NS create token $SA --duration=8760h)
tmp=$(mktemp -d)
cat > "$tmp/kubeconfig" <<EOF
apiVersion: v1
kind: Config
current-context: default
clusters:
  - name: default
    cluster:
      server: https://kubernetes.default.svc
      insecure-skip-tls-verify: true
users:
  - name: default
    user:
      token: $TOKEN
contexts:
  - name: default
    context:
      cluster: default
      user: default
EOF

kubectl --context $CTX -n $NS create secret generic "$PM--$CLUSTER_ID" \
  --from-file=kubeconfig="$tmp/kubeconfig" --dry-run=client -o yaml \
  | kubectl --context $CTX label --local -f - -o yaml \
      deploy.platform-mesh.io/rootshard=true \
      deploy.platform-mesh.io/frontproxy=true \
      deploy.platform-mesh.io/shards-default=true \
  | kubectl --context $CTX apply -f -
rm -rf "$tmp"
```

A ServiceAccount token with `insecure-skip-tls-verify` rather than the admin
credential and the cluster CA: the pod dials `kubernetes.default.svc`, whose
serving cert is signed by the CA this kubeconfig would otherwise have to carry.

## 8. The installation itself

`hostnameTemplate`, the etcd endpoints and the etcd prefix are **CEL
expressions**, so a literal string appears as a quoted string inside the YAML
string. `platformMesh`, `component` and `cluster` are bound per shard.

```bash
kubectl --context $CTX apply -f - <<EOF
apiVersion: deploy.platform-mesh.io/v1alpha1
kind: RootShardTemplate
metadata:
  name: root
  namespace: $NS
spec:
  etcd:
    endpoints:
      - '"https://etcd.$CLUSTER_ID.sslip.io:$PORT"'
    prefix: '"/" + platformMesh + "/root"'
    tlsConfig:
      secretRef:
        name: etcd-client-tls
  certificates:
    issuerRef:
      name: kcp
      kind: ClusterIssuer
      group: cert-manager.io
---
apiVersion: deploy.platform-mesh.io/v1alpha1
kind: ShardTemplate
metadata:
  name: default
  namespace: $NS
spec:
  etcd:
    endpoints:
      - '"https://etcd.$CLUSTER_ID.sslip.io:$PORT"'
    prefix: '"/" + platformMesh + "/" + component + "/" + cluster'
    tlsConfig:
      secretRef:
        name: etcd-client-tls
---
apiVersion: deploy.platform-mesh.io/v1alpha1
kind: PlatformMesh
metadata:
  name: $PM
  namespace: $NS
spec:
  version: "0.0.0"
  ocm:
    # Never resolved here: no OCMModule references it, and the topology half of
    # the deployer does not consult OCM at all.
    url: oci://example.com/platform-mesh
  ingress:
    - name: gateway
      type: gatewayapi
      gatewayAPI:
        gatewayName: eg
        gatewayNamespace: envoy-gateway-system
        sectionName: passthrough
  topology:
    rootShard:
      name: root
      templateRef:
        name: root
      exposure:
        hostnameTemplate: '"root." + cluster + ".sslip.io"'
        port: $PORT
      virtualWorkspaces:
        exposure:
          hostnameTemplate: '"vw-root." + cluster + ".sslip.io"'
          port: $PORT
    shardGroups:
      - name: default
        templateRef:
          name: default
        exposure:
          hostnameTemplate: 'component + "." + cluster + ".sslip.io"'
          port: $PORT
        virtualWorkspaces:
          exposure:
            hostnameTemplate: '"vw." + component + "." + cluster + ".sslip.io"'
            port: $PORT
    frontProxy:
      name: fp
      exposure:
        hostnameTemplate: '"fp." + cluster + ".sslip.io"'
        port: $PORT
EOF
```

No `auth.oidc` block, so the only identities are the operator's own
certificates. Add one under `RootShardTemplate.spec.auth` if you need human
logins.

Watch it build (a minute or two from `Provisioning` to `Running`):

```bash
kubectl --context $CTX -n $NS get platformmesh,rootshards.operator.kcp.io,\
shards.operator.kcp.io,frontproxies.operator.kcp.io
kubectl --context $CTX -n $NS get pods

# Wait for kcp to actually serve. The front proxy CR exists — and mints
# kubeconfigs — well before its pods accept connections, so without this the
# first request fails with `connection reset by peer`.
kubectl --context $CTX -n $NS wait frontproxies.operator.kcp.io --all \
  --for=jsonpath='{.status.phase}'=Running --timeout=10m
```

`ClientCA certificate ... not found` in the kcp-operator log during the first
~30s is cert-manager still issuing, not a failure.

## 9. An admin kubeconfig

The front proxy's name is generated (`names.FrontProxy()` hashes the
PlatformMesh, the front proxy and the cluster ID), so select on the labels the
deployer stamps rather than writing the name down. Ask kcp-operator to mint a
credential of your own rather than borrowing the `$PM-provisioner` Kubeconfig the
deployer made for itself — revoking that one breaks the running operator.

```bash
until FP=$(kubectl --context $CTX -n $NS get frontproxies.operator.kcp.io \
      -l deploy.platform-mesh.io/platform-mesh=$PM,deploy.platform-mesh.io/component=frontproxy \
      -o jsonpath='{.items[0].metadata.name}' 2>/dev/null) && [ -n "$FP" ]; do sleep 5; done

kubectl --context $CTX apply -f - <<EOF
apiVersion: operator.kcp.io/v1alpha1
kind: Kubeconfig
metadata:
  name: $PM-admin
  namespace: $NS
spec:
  target:
    frontProxyRef:
      name: $FP
  targetWorkspace: root
  username: manual-admin
  groups:
    - system:kcp:admin
  validity: 720h
  secretRef:
    name: $PM-admin-kubeconfig
EOF

kubectl --context $CTX -n $NS wait kubeconfig/$PM-admin --for=condition=Available --timeout=5m
kubectl --context $CTX -n $NS get secret/$PM-admin-kubeconfig \
  -o jsonpath='{.data.kubeconfig}' | base64 -d > /tmp/$PM-admin.kubeconfig

KUBECONFIG=/tmp/$PM-admin.kubeconfig kubectl get workspaces
```

The kubeconfig points at `https://fp.$CLUSTER_ID.sslip.io:$PORT`, which resolves
and routes from the host as well as from inside the cluster — no port-forward.

## Teardown

```bash
kind delete cluster --name pm-manual
```

## Differences from the other two paths

- **Tilt** deploys this same `config/default`, minus the Namespace (the base env
  owns it) and plus `--log-level=debug`, and rebuilds the binary on change. It
  also brings dex and the rest of the platform; this doc stops at kcp.
- **`tests/e2e/deployer`** applies the same bases but runs the deployer **on the host**,
  in-process, with a custom `KcpDial` — the sslip.io front-proxy hostname is
  blocked by DNS rebind protection on the host. In-cluster, as here, that
  workaround is unnecessary.
