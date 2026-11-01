# gateway-api-crds

Helm chart for Kubernetes Gateway API CRDs (experimental channel)

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## About

This chart ships the Kubernetes [Gateway API](https://gateway-api.sigs.k8s.io/) Custom Resource Definitions from the **experimental** release channel. The CRDs are vendored verbatim under `templates/` so they can be published as an OCM Helm chart artifact and deployed via a `HelmRelease`.

The CRDs are pinned to a specific upstream release and commit — they are not templated and take no values.

| Field | Value                                                                                                                              |
|-------|------------------------------------------------------------------------------------------------------------------------------------|
| Upstream | [kubernetes-sigs/gateway-api](https://github.com/kubernetes-sigs/gateway-api)                                                      |
| Version | `v1.5.1`                                                                                                                           |
| Channel | experimental (`config/crd/experimental`)                                                                                           |
| Pinned commit | [`477d172e`](https://github.com/kubernetes-sigs/gateway-api/tree/e7677b70ae75d14a4448fba94870e7deea6cf0ad/config/crd/experimental) |

## Updating CRDs

CRDs are vendored, not generated. To upgrade, replace the files in `templates/` with the manifests from the target upstream release, then update the pinned version and commit above and bump `version` in `Chart.yaml` to match the upstream release.

```bash
REF=e7677b70ae75d14a4448fba94870e7deea6cf0ad
BASE="https://raw.githubusercontent.com/kubernetes-sigs/gateway-api/${REF}/config/crd/experimental"
for f in \
  gateway.networking.k8s.io_backendtlspolicies.yaml \
  gateway.networking.k8s.io_gatewayclasses.yaml \
  gateway.networking.k8s.io_gateways.yaml \
  gateway.networking.k8s.io_grpcroutes.yaml \
  gateway.networking.k8s.io_httproutes.yaml \
  gateway.networking.k8s.io_referencegrants.yaml \
  gateway.networking.k8s.io_tcproutes.yaml \
  gateway.networking.k8s.io_tlsroutes.yaml \
  gateway.networking.k8s.io_udproutes.yaml \
  gateway.networking.k8s.io_vap_safeupgrades.yaml \
  gateway.networking.x-k8s.io_xbackendtrafficpolicies.yaml \
  gateway.networking.k8s.io_listenersets.yaml \
  gateway.networking.x-k8s.io_xmeshes.yaml; do
  curl -fsSL "${BASE}/${f}" -o "charts/gateway-api-crds/templates/${f}"
done
```

After updating, regenerate the snapshot tests:

```bash
helm unittest -u charts/gateway-api-crds
```
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|

## Overriding Values

The values in the `defaults:` section can be reused from other charts by using the lookup function "common.getKeyValue". It implements lookup on three levels:

1. Looks for `keyOverride` in the chart's values.yaml
2. Looks for `global.key` in the chart's or parent chart's values.yaml
3. Uses the `key` in the chart's values.yaml
4. Uses the `common.defaults.key` value from the table below.

1 has precedence over 2 over 3 over 4 respectively. This approach allows for individual charts to have minimal configuration, while still being able to override parameters locally.

Example
```
1) .Values.deployment.resources.limits.memoryOverride = 4096MB
2) .Values.global.deployment.resources.limits.memory = 2048MB
3) .Values.deployment.resources.limits.memory = 1024MB
4) .Values.common.defaults.deployment.resources.limits.memory = default 512MB
```
