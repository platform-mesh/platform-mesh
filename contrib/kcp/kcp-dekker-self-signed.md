# Scenario 1: Self-Signed Certificates (kcp-dekker)

This scenario deploys kcp in a single Kubernetes cluster using self-signed certificates. This configuration is ideal for development, testing, or closed internal environments where external certificate authorities are not required.

## Architecture Overview

- **Certificate Strategy**: All certificates are self-signed using an internal CA
- **Access Pattern**: Only front-proxy is publicly accessible, shards are private
- **Namespace**: `kcp-dekker`
- **Use Case**: Development, testing, or internal environments where you control certificate trust

## Prerequisites

Before starting, ensure you have completed the [shared components setup](0-shared.md).

## Deployment Steps

### 1. Create Namespace and etcd Certificates

Create the deployment namespace and configure certificates for etcd:

```bash
kubectl create namespace kcp-dekker
kubectl apply -f kcp/assets/kcp-dekker/certificate-etcd.yaml
```

### 2. Deploy etcd Clusters

Deploy the etcd clusters for both root and alpha shards:

```bash
kubectl apply -f kcp/assets/kcp-dekker/etcd-druid-root.yaml
kubectl apply -f kcp/assets/kcp-dekker/etcd-druid-alpha.yaml
```

### 3. Configure KCP System Certificates

Create the certificate authorities and issuers for kcp components:

```bash
kubectl apply -f kcp/assets/kcp-dekker/certificate-kcp.yaml
```

### 4. Deploy KCP Components

Deploy the kcp shards and front-proxy:

```bash
kubectl apply -f kcp/assets/kcp-dekker/kcp-root-shard.yaml
kubectl apply -f kcp/assets/kcp-dekker/kcp-alpha-shard.yaml
kubectl apply -f kcp/assets/kcp-dekker/kcp-front-proxy.yaml
```

### 5. Configure DNS for Front-Proxy

The front-proxy certificate is initially missing because it depends on the LoadBalancer IP. Configure DNS:

1. **Get the LoadBalancer IP**:
   ```bash
   kubectl get svc -n kcp-dekker frontproxy-front-proxy
   ```

2. **Create DNS A record** pointing `api.dekker.example.com` to the LoadBalancer IP

3. **Verify certificate issuance**:
   ```bash
   kubectl get certificate -n kcp-dekker root-frontproxy-server -o yaml
   ```

### 6. Create Admin Kubeconfig

Generate the kubeconfig for administrative access:

```bash
kubectl apply -f kcp/assets/kcp-dekker/kubeconfig-kcp-admin.yaml
```

### 7. Test Access

Extract the kubeconfig and verify kcp functionality:

```bash
kubectl get secret -n kcp-dekker kcp-admin-frontproxy \
  -o jsonpath='{.data.kubeconfig}' | base64 -d > kcp-admin-kubeconfig-dekker.yaml

KUBECONFIG=kcp-admin-kubeconfig-dekker.yaml kubectl get shards
```

Expected output:
```
NAME    REGION   URL                                                           EXTERNAL URL                                       AGE
alpha            https://alpha-shard-kcp.kcp-dekker.svc.cluster.local:6443   https://api.dekker.example.com:6443   14m
root             https://root-kcp.kcp-dekker.svc.cluster.local:6443          https://api.dekker.example.com:6443   14m
```


## Optional: OIDC Authentication

If you deployed the optional OIDC provider (Dex) in the shared components, you can configure OIDC authentication for kcp.

### Install kubectl OIDC Plugin

```bash
# macOS
brew install int128/kubelogin/kubelogin

# For other platforms, see: https://github.com/int128/kubelogin
```

### Configure OIDC Credentials

Configure kubectl to use OIDC authentication:

```bash
kubectl config set-credentials oidc \
  --exec-api-version=client.authentication.k8s.io/v1beta1 \
  --exec-command=kubectl \
  --exec-arg=oidc-login \
  --exec-arg=get-token \
  --exec-arg=--oidc-issuer-url="https://auth.example.com" \
  --exec-arg=--oidc-client-id="platform-mesh" \
  --exec-arg=--oidc-extra-scope="email" \
  --exec-arg=--oidc-redirect-url=http://127.0.0.1:8000/ \
  --exec-arg=--oidc-client-secret=<YOUR_CLIENT_SECRET>

kubectl config set-context --current --user=oidc
```

**Important**: Replace `<YOUR_CLIENT_SECRET>` with your actual OIDC client secret.

### Test OIDC Authentication

When you run `kubectl get shards`, you should be redirected to your OIDC provider (GitHub via Dex) for authentication. Note that newly authenticated users will not have permissions until they are granted appropriate RBAC roles.

## Next Steps

Your kcp deployment with self-signed certificates is complete. You can now:
- Configure user access and RBAC permissions
- Deploy workloads to your kcp shards
- Set up additional controllers or operators

## Troubleshooting

**Certificate Trust Issues**: If clients cannot connect due to certificate validation errors, ensure the self-signed CA certificate is added to the client's trust store.

**DNS Resolution**: Verify that `api.dekker.example.com` resolves to the correct LoadBalancer IP.
