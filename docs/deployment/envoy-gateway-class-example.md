# Running Platform Mesh with Envoy Gateway

Platform Mesh uses the [Gateway API](https://gateway-sigs.k8s.io/) for all
ingress. The charts emit only standard `Gateway`, `HTTPRoute`, and `TLSRoute`
resources, so any conformant controller works. This guide uses Envoy Gateway as
the concrete example, covering both a local Kind cluster and a production cluster.

---

## 1. Prepare the cluster

<details open>
<summary><strong>Kind</strong></summary>

The Kind config maps host port `8443` to NodePort `31000`. The Envoy proxy
Service is configured to use that NodePort in the next step.

```shell
kind create cluster --name platform-mesh --image kindest/node:v1.35.1 --config=- <<'EOF'
apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
networking:
  apiServerAddress: "127.0.0.1"
  kubeProxyMode: iptables
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 31000
        hostPort: 8443
        protocol: TCP
        listenAddress: "127.0.0.1"
EOF
```

</details>

<details>
<summary><strong>Production</strong></summary>

Confirm that `kubectl` targets the intended cluster:

```shell
kubectl config current-context
```

</details>

## 2. Install the prerequisites

<details open>
<summary><strong>Kind</strong></summary>

```shell
helm upgrade --install platform-mesh-prerequisites \
  oci://ghcr.io/platform-mesh/helm-charts/platform-mesh-prerequisites \
  --namespace platform-mesh-system \
  --create-namespace \
  --set keycloak-operator.watchNamespaces=platform-mesh-system \
  --wait \
  --timeout 15m
```

</details>

<details>
<summary><strong>Production</strong></summary>

```shell
helm upgrade --install platform-mesh-prerequisites \
  oci://ghcr.io/platform-mesh/helm-charts/platform-mesh-prerequisites \
  --namespace platform-mesh-system \
  --create-namespace \
  --wait \
  --timeout 15m
```

</details>

## 3. Install Envoy Gateway and configure the GatewayClass

Install Envoy Gateway:

```shell
helm upgrade --install envoy-gateway \
  oci://docker.io/envoyproxy/gateway-helm \
  --namespace envoy-gateway-system \
  --create-namespace \
  --wait
```

Apply the `EnvoyProxy` configuration and `GatewayClass`.

<details open>
<summary><strong>Kind</strong></summary>

The `EnvoyProxy` CR pins the proxy Service to NodePort `31000` and ClusterIP
`10.96.188.4` so the Kind port-forward works and PMO can reach the gateway from
within the cluster. Port `443` is added alongside `8443` because some Platform
Mesh components construct `https://<baseDomain>/...` without an explicit port.

```shell
kubectl apply -f - <<'EOF'
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: EnvoyProxy
metadata:
  name: local-nodeport
  namespace: envoy-gateway-system
spec:
  provider:
    type: Kubernetes
    kubernetes:
      envoyService:
        type: NodePort
        patch:
          type: StrategicMerge
          value:
            spec:
              clusterIP: 10.96.188.4
              ports:
                - name: https-8443
                  port: 8443
                  targetPort: 8443
                  nodePort: 31000
                - name: https-443
                  port: 443
                  targetPort: 8443
---
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: envoy
spec:
  controllerName: gateway.envoyproxy.io/gatewayclass-controller
  parametersRef:
    group: gateway.envoyproxy.io
    kind: EnvoyProxy
    name: local-nodeport
    namespace: envoy-gateway-system
EOF

kubectl wait gatewayclass/envoy --for=condition=Accepted --timeout=60s
```

</details>

<details>
<summary><strong>Production</strong></summary>

The `EnvoyProxy` CR configures a LoadBalancer Service. DNS for the base domain
and its wildcard must resolve to the LoadBalancer IP once it is assigned.

```shell
kubectl apply -f - <<'EOF'
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: EnvoyProxy
metadata:
  name: loadbalancer
  namespace: envoy-gateway-system
spec:
  provider:
    type: Kubernetes
    kubernetes:
      envoyService:
        type: LoadBalancer
---
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: envoy
spec:
  controllerName: gateway.envoyproxy.io/gatewayclass-controller
  parametersRef:
    group: gateway.envoyproxy.io
    kind: EnvoyProxy
    name: loadbalancer
    namespace: envoy-gateway-system
EOF

kubectl wait gatewayclass/envoy --for=condition=Accepted --timeout=60s

until load_balancer_address=$(kubectl --namespace envoy-gateway-system \
  get service envoy-gateway \
  --output jsonpath='{.status.loadBalancer.ingress[0].ip}{.status.loadBalancer.ingress[0].hostname}') \
  && [ -n "${load_balancer_address}" ]; do
  echo "Waiting for LoadBalancer address..."
  sleep 5
done
echo "LoadBalancer address: ${load_balancer_address}"
echo "Configure ${base_domain} and *.${base_domain} to point to ${load_balancer_address}"
```

</details>

## 4. Install Platform Mesh

<details open>
<summary><strong>Kind</strong></summary>

```shell
base_domain=portal.localhost
platform_mesh_version=0.6.0-build.6

helm upgrade --install platform-mesh-operator \
  oci://ghcr.io/platform-mesh/helm-charts/platform-mesh-operator \
  --namespace platform-mesh-system \
  --set ocm.enabled=true \
  --set ocm.profile=kind \
  --set gateway.className=envoy \
  --set gateway.baseDomain="${base_domain}" \
  --set gateway.port=8443 \
  --set gateway.clusterIP=10.96.188.4 \
  --set kcp.frontProxyPort=8443 \
  --set certificates.selfSigned.enabled=true \
  --set-string ocm.version="${platform_mesh_version}" \
  --wait \
  --timeout 15m
```

</details>

<details>
<summary><strong>Production</strong></summary>

```shell
base_domain=platform.example.com
platform_mesh_version=0.6.0-build.6

helm upgrade --install platform-mesh-operator \
  oci://ghcr.io/platform-mesh/helm-charts/platform-mesh-operator \
  --namespace platform-mesh-system \
  --set ocm.enabled=true \
  --set ocm.profile=production \
  --set gateway.className=envoy \
  --set gateway.baseDomain="${base_domain}" \
  --set gateway.port=443 \
  --set kcp.frontProxyPort=443 \
  --set certificates.selfSigned.enabled=true \
  --set-string ocm.version="${platform_mesh_version}" \
  --wait \
  --timeout 15m
```

</details>

## 5. Wait for Platform Mesh

```shell
kubectl wait --namespace platform-mesh-system \
  --for=condition=Ready platformmeshes/platform-mesh \
  --timeout=30m
```

Open the Platform Mesh endpoint. For Kind: `https://${base_domain}:8443`. For
production: `https://${base_domain}`.
