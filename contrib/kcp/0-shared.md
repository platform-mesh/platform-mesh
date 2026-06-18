# Shared Components Setup

This guide covers the installation of shared components required for all kcp deployment scenarios. These components provide the foundation for kcp operations including database storage, certificate management, and authentication.

## Prerequisites

- A Kubernetes cluster with sufficient resources
- `kubectl` configured to access your cluster  
- `helm` CLI tool installed
- DNS management capability (manual or automated)
- (Optional) CloudFlare account for DNS01 challenges

## Template Files

Files with `.template` suffix require customization for your environment. Copy them without the `.template` suffix and modify the values:

```bash
cp kcp/assets/cert-manager/cloudflare-secret.yaml.template \
   kcp/assets/cert-manager/cloudflare-secret.yaml
# Edit the copied file with your actual values
```

## Certificate Management Strategy

We use `cert-manager` with Let's Encrypt for certificate issuance. DNS management is manual, where external IP is allocated through Kubernetes LoadBalancer services and added to external DNS manually.

**Note**: For production environments, consider automating DNS management with `external-dns`.

### DNS01 Challenge Configuration
For CloudFlare DNS integration, the issuer uses this configuration:

```yaml
solvers:
- dns01:
    cloudflare:
      email: your-email@example.com
      apiKeySecretRef:
        name: cloudflare-api-key-secret
        key: api-key
```

For other DNS providers, adjust the solver configuration accordingly.

## 1. etcd Database

kcp requires an etcd cluster for persistent storage. For production deployments, we recommend using [etcd-druid](https://github.com/gardener/etcd-druid) which provides automated etcd cluster management.

### Install etcd-druid Operator

```bash
helm install etcd-druid oci://europe-docker.pkg.dev/gardener-project/releases/charts/gardener/etcd-druid \
  --namespace etcd-druid \
  --create-namespace \
  --version v0.32.0
```

### Install Required CRDs

**Known Issue**: The etcd-druid chart doesn't install CRDs automatically. Install them manually:
([Issue #1185](https://github.com/gardener/etcd-druid/issues/1185))

```bash
kubectl apply -f https://gist.githubusercontent.com/mjudeikis/94382cc47c6a8611a2a1b85e20ec8380/raw/953478d8605bcb3419de2a6958147a5fca932b20/etcdcopybackupstasks.druid.gardener.cloud
kubectl apply -f https://gist.githubusercontent.com/mjudeikis/94382cc47c6a8611a2a1b85e20ec8380/raw/953478d8605bcb3419de2a6958147a5fca932b20/etcds.druid.gardener.cloud
```

## 2. Certificate Manager

cert-manager handles certificate lifecycle for both etcd and kcp components. It integrates with Let's Encrypt for automatic certificate issuance and renewal.

### Install cert-manager

```bash
helm repo add jetstack https://charts.jetstack.io
helm repo update

helm upgrade \
  --install \
  --namespace cert-manager \
  --create-namespace \
  --version v1.18.2 \
  --set crds.enabled=true \
  --atomic \
  cert-manager jetstack/cert-manager
```

### Configure Let's Encrypt Issuer

Apply the cluster issuer for Let's Encrypt certificate provisioning:

```bash
kubectl apply -f kcp/assets/cert-manager/cluster-issuer.yaml
```

### Configure DNS Provider (CloudFlare Example)

For DNS01 challenges, configure your DNS provider credentials. For CloudFlare:

1. **Create API Secret**: Edit and apply the CloudFlare API secret:
   ```bash
   kubectl apply -f kcp/assets/cert-manager/cloudflare-secret.yaml
   ```

2. **Reference Documentation**: See [CloudFlare DNS01 Configuration](https://cert-manager.io/docs/configuration/acme/dns01/cloudflare/#api-keys) for detailed setup instructions.

**Note**: For other DNS providers, modify the cluster-issuer.yaml configuration accordingly.

## 3. etcd PKI Setup

etcd requires PKI certificates for secure communication. Since etcd-druid doesn't handle certificate provisioning automatically, we use cert-manager to create the necessary certificate issuers.

### Configure etcd Certificate Issuer

Create the certificate issuer in the cert-manager namespace:

```bash
kubectl apply -f kcp/assets/etcd-druid/certificate-etcd-issuer.yaml
```

**Note**: These issuers are created in the `cert-manager` namespace by default. You can modify the namespace by reconfiguring cert-manager if needed.

**Related Issue**: See [etcd-druid #1187](https://github.com/gardener/etcd-druid/issues/1187) for details about manual PKI setup requirements.

## 4. KCP Operator

The kcp-operator manages the lifecycle of kcp components including shards, front-proxies, and kubeconfigs.

### Install kcp-operator

```bash
helm repo add kcp https://kcp-dev.github.io/helm-charts

helm upgrade --install \
  --create-namespace \
  --namespace kcp-operator \
  kcp-operator kcp/kcp-operator
```

## 5. OIDC Provider (Optional)

If you have an existing OIDC provider, you can skip this section. This guide uses Dex as the OIDC provider with PostgreSQL as the backend database.

### 5.1. Install PostgreSQL Operator

Create the OIDC namespace and install CloudNative PostgreSQL operator:

```bash
kubectl create namespace oidc

kubectl apply --server-side -f \
  https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.26/releases/cnpg-1.26.0.yaml
```

### 5.2. Deploy PostgreSQL Database

Create a PostgreSQL cluster and database for Dex:

```bash
kubectl apply -f kcp/assets/oidc-dex/postgres-cluster.yaml
kubectl apply -f kcp/assets/oidc-dex/postgres-database.yaml
```

### 5.3. Configure Dex Certificates

Request a certificate for Dex from cert-manager:

```bash
kubectl apply -f kcp/assets/oidc-dex/certificate-dns.yaml
```

### 5.4. Install Dex OIDC Provider

Wait for the certificate to be issued, then install Dex:

```bash
# Check certificate status
kubectl get certificate -n oidc

# Install Dex
helm repo add dex https://charts.dexidp.io

helm upgrade -i dex dex/dex \
  --create-namespace \
  --namespace oidc \
  -f kcp/assets/oidc-dex/values.yaml
```

**Note**: Ensure you customize `kcp/assets/oidc-dex/values.yaml` with your specific OIDC configuration before installation.

## 6. DNS Configuration

kcp requires custom domain names for the front-proxy and shard endpoints. This setup uses Kubernetes LoadBalancer services with manual DNS record management.

### DNS Requirements

Each kcp deployment scenario requires domain names for:
- **Front-proxy**: Main API endpoint for client access
- **Shard endpoints**: Direct access to individual kcp shards (for some scenarios)

### DNS Setup Approach

We use **LoadBalancer Services + Manual DNS Records** for simplicity and reliability:

1. **Deploy kcp components** with LoadBalancer services
2. **Get LoadBalancer IPs** from Kubernetes
3. **Create DNS A records** pointing to these IPs

### Example Domain Structure

For a deployment in the `dekker` scenario:
```
# Front-proxy domain
api.dekker.example.com -> LoadBalancer IP

# Shard domains (if public access needed)
root.dekker.example.com -> Root shard LoadBalancer IP  
alpha.dekker.example.com -> Alpha shard LoadBalancer IP
```

### DNS Automation (Optional)

For production environments, consider automating DNS management with:
- [external-dns](https://github.com/kubernetes-sigs/external-dns) controller
- Cloud provider DNS integration
- GitOps-based DNS record management

## Next Steps

Your cluster is now ready to host kcp. Proceed to your chosen deployment scenario:
- [Scenario 1: Self-Signed Certificates](kcp-dekker-self-signed.md)
- [Scenario 2: External Certificates](kcp-vespucci-external-certs.md)  
- [Scenario 3: Dual Front-Proxy](kcp-comer-dual-frontproxy.md)
