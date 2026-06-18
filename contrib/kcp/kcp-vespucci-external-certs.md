# Alternative: External Certificates (kcp-vespucci)

This variation of the [base Columbus setup](README.md) uses Let's Encrypt certificates for the front-proxy while keeping internal certificates for shards. This enables production deployments with external controller access.

## When to Use This Variation

- **Production environments** requiring automatic certificate renewal
- **External controllers** that need to access kcp from outside the cluster  
- **Mixed access patterns** where you want public front-proxy access but can accept internal certificates for shards

## Key Differences from Base Setup

| Aspect | Base (Columbus) | This Variation (Vespucci) |
|--------|-----------------|---------------------------|
| **Namespace** | `kcp-dekker` | `kcp-vespucci` |
| **Front-proxy Certs** | Self-signed | Let's Encrypt (automatic) |
| **Shard Access** | Private cluster-only | Publicly accessible |
| **Certificate Trust** | Manual CA trust required | Automatic for front-proxy |
| **External Controllers** | Not supported | Fully supported |

## Prerequisites

Complete the [base setup prerequisites](README.md#prerequisites) first.

## Setup Changes from Base

Follow the same deployment steps as the [base setup](README.md#deployment-steps-kcp-dekker), but with these modifications:

### Modified Steps

**Step 1**: Use the `kcp-vespucci` namespace instead:
```bash
kubectl create namespace kcp-vespucci
```

**Step 1-5**: Replace `kcp-dekker` with `kcp-vespucci` in all asset file paths:
```bash
kubectl apply -f kcp/assets/kcp-vespucci/certificate-etcd.yaml
kubectl apply -f kcp/assets/kcp-vespucci/etcd-druid-root.yaml
kubectl apply -f kcp/assets/kcp-vespucci/etcd-druid-alpha.yaml
kubectl apply -f kcp/assets/kcp-vespucci/certificate-kcp.yaml
kubectl apply -f kcp/assets/kcp-vespucci/kcp-root-shard.yaml
kubectl apply -f kcp/assets/kcp-vespucci/kcp-alpha-shard.yaml
kubectl apply -f kcp/assets/kcp-vespucci/kcp-front-proxy.yaml
```

**DNS Configuration**: Use `api.vespucci.example.com` instead of the Columbus domain.

### Additional Setup Required

**Let's Encrypt CA for kubectl**: Since kubectl needs explicit CA configuration for Let's Encrypt:

```bash
curl -L -o isrgrootx1.pem https://letsencrypt.org/certs/isrgrootx1.pem
kubectl create secret generic letsencrypt-ca --from-file=tls.crt=isrgrootx1.pem -n kcp-vespucci
```

**Final Verification**:
```bash
kubectl apply -f kcp/assets/kcp-vespucci/kubeconfig-kcp-admin.yaml

kubectl get secret -n kcp-vespucci kcp-admin-frontproxy \
  -o jsonpath='{.data.kubeconfig}' | base64 -d > kcp-admin-kubeconfig-vespucci.yaml

KUBECONFIG=kcp-admin-kubeconfig-vespucci.yaml kubectl get shards
```

Expected output shows publicly accessible shard URLs:
```
NAME    REGION   URL                                                  EXTERNAL URL                                       AGE
alpha            https://alpha.vespucci.example.com:6443   https://api.vespucci.example.com:6443   2d19h
root             https://root.vespucci.example.com:6443    https://api.vespucci.example.com:6443   2d20h
```

## What You Gain with This Variation

- **Automatic Certificate Renewal**: Let's Encrypt handles certificate lifecycle
- **External Controller Support**: Controllers can run outside the cluster with public shard access
- **Trusted Certificates**: Browsers and most tools automatically trust Let's Encrypt certificates
- **Production Ready**: Suitable for production environments with external integrations

## OIDC Authentication (Optional)

OIDC setup is identical to the [base setup](README.md#optional-oidc-authentication).

## Troubleshooting

**Certificate Issuance**: If Let's Encrypt certificates fail to be issued, verify DNS propagation and CloudFlare API credentials are correct.

**Mixed Certificate Trust**: External controllers may need CA bundle configuration to trust both external (Let's Encrypt) and internal certificates when accessing shards directly.
