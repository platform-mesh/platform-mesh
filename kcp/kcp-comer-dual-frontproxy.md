# Alternative: Dual Front-Proxy with Edge Re-encryption (kcp-comer)

This advanced variation of the [base Columbus setup](README.md) deploys dual front-proxy instances for CDN integration with edge re-encryption. This is specifically designed for CloudFlare or similar CDN services where certificate authentication doesn't work with TLS termination at the edge.

## When to Use This Variation

- **CDN Integration**: Need CloudFlare, Cloudfront, or similar CDN services
- **Edge Re-encryption**: TLS terminates at CDN edge, re-encrypts to internal front-proxy
- **DDoS Protection**: Require CDN-level protection and traffic management
- **Complex Certificate Chains**: Certificate authentication breaks with edge TLS termination

## Key Differences from Base Setup

| Aspect | Base (Columbus) | This Variation (Comer) |
|--------|-----------------|------------------------|
| **Namespace** | `kcp-dekker` | `kcp-comer` |
| **Front-proxy Count** | Single | Dual (public + internal) |
| **CDN Integration** | None | Full CloudFlare support |
| **Certificate Chain** | Simple self-signed | Edge + internal certificates |
| **Recommended Auth** | Certificate-based | OIDC (due to cert complexity) |
| **Complexity** | Simple | Advanced |

## Prerequisites

Complete the [base setup prerequisites](README.md#prerequisites) first.

## Setup Changes from Base

Follow the same deployment steps as the [base setup](README.md#deployment-steps-kcp-dekker), but with these modifications:

### Additional Preparation Required

**CloudFlare CA Certificate**: Download the CloudFlare edge certificate for extended trust:

**Important**: Verify this URL with CloudFlare documentation before production use.

```bash
curl -L -o google-we1.pem https://ssl-tools.net/certificates/108fbf794e18ec5347a414e4370cc4506c297ab2.pem
kubectl create namespace kcp-comer
kubectl create secret generic google-we1-ca --from-file=tls.crt=google-we1.pem -n kcp-comer
```

### Modified Steps

**Step 1-3**: Replace `kcp-dekker` with `kcp-comer` in all asset file paths:
```bash
kubectl apply -f kcp/assets/kcp-comer/certificate-etcd.yaml
kubectl apply -f kcp/assets/kcp-comer/etcd-druid-root.yaml
kubectl apply -f kcp/assets/kcp-comer/etcd-druid-alpha.yaml
kubectl apply -f kcp/assets/kcp-comer/certificate-kcp.yaml
```

**Step 4**: Deploy both front-proxy instances plus shards:
```bash
kubectl apply -f kcp/assets/kcp-comer/kcp-root-shard.yaml
kubectl apply -f kcp/assets/kcp-comer/kcp-alpha-shard.yaml
kubectl apply -f kcp/assets/kcp-comer/kcp-front-proxy.yaml
kubectl apply -f kcp/assets/kcp-comer/kcp-front-proxy-internal.yaml
```

**Step 5**: Configure DNS for ALL services (not just front-proxy):
```bash
# Get LoadBalancer IPs for all services
kubectl get svc -n kcp-comer frontproxy-front-proxy
kubectl get svc -n kcp-comer frontproxy-internal-front-proxy
kubectl get svc -n kcp-comer root-shard
kubectl get svc -n kcp-comer alpha-shard
```

Create DNS A records (or CNAME if Loadbalancer is DNS based) for all services using their LoadBalancer IPs.

### Additional Steps Required

**Step 6**: Create kubeconfigs for both access patterns:
```bash
kubectl apply -f kcp/assets/kcp-comer/kubeconfig-kcp-admin.yaml
kubectl apply -f kcp/assets/kcp-comer/kubeconfig-kcp-admin-internal.yaml
```

**CloudFlare Configuration**: Configure your CloudFlare dashboard:

1. **Set `api.comer` to "Proxied"** (orange cloud icon)
2. **Add Page Rule**: "Rewrite port to 6443" for the API domain
3. **Upload Custom CA** in SSL/TLS tab so CloudFlare trusts the internal certificate:
   ```bash
   kubectl get secret -n kcp-comer root-ca -o jsonpath='{.data.ca\.crt}' | base64 -d
   ```

## Important: Certificate Authentication Limitation

Due to CloudFlare's certificate re-encryption, certificate-based authentication through the public front-proxy **will not work**. The certificate presented to clients is CloudFlare's certificate, not the internal front-proxy certificate.

**Solution**: Use OIDC authentication for external access.

## Required: OIDC Authentication for External Access

Set up OIDC following the same process as the [base setup](README.md#optional-oidc-authentication), then test access:

```bash
# Extract public kubeconfig
kubectl get secret -n kcp-comer kcp-admin-frontproxy \
  -o jsonpath='{.data.kubeconfig}' | base64 -d > kcp-admin-kubeconfig-comer.yaml

# This will not work, as it needs OIDC pre-configured

# Test with OIDC (should redirect to authentication)
KUBECONFIG=kcp-admin-kubeconfig-comer.yaml kubectl get shards
```
Expected output shows edge-proxied URLs:
```
NAME    REGION   URL                                               EXTERNAL URL                                   AGE
alpha            https://alpha.comer.example.com:6443   https://api.comer.example.com:443   16m
root             https://root.comer.example.com:6443    https://api.comer.example.com:443   16m
```

### Internal Access Still Works with Certificates

```bash
kubectl get secret -n kcp-comer kcp-admin-frontproxy-internal \
  -o jsonpath='{.data.kubeconfig}' | base64 -d > kcp-admin-kubeconfig-comer-internal.yaml

KUBECONFIG=kcp-admin-kubeconfig-comer-internal.yaml kubectl get shards
```

## What You Gain with This Variation

- **CDN Protection**: Full CloudFlare DDoS protection and edge optimization
- **Dual Access Patterns**: External users via CDN, internal services via direct connection
- **Enterprise Security**: Edge re-encryption with multiple certificate validation layers
- **Traffic Management**: CloudFlare rules for routing, rate limiting, and geo-blocking

## When You Need This Setup

- Production environments requiring CDN acceleration
- Organizations using CloudFlare for DDoS protection
- Compliance requirements for edge security
- Mixed access patterns (external users + internal controllers)

## Troubleshooting

**CloudFlare Trust**: Ensure internal CA is uploaded to CloudFlare SSL settings.

**Port Rewriting**: Verify CloudFlare page rules rewrite to port 6443.

**Certificate Chain**: Remember that certificate auth only works via internal front-proxy.
