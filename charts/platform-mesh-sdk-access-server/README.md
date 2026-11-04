# platform-mesh-sdk-access-server

A Helm chart to deploy the Platform Mesh access SDK HTTP server on a user-side cluster.

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| accessPort | int | `8443` | Port the /access listener binds to inside the Pod. Exposed externally via the Service below. |
| crds.enabled | bool | `false` |  |
| deployment.replicas | int | `1` |  |
| deployment.specTemplate.annotations | object | `{}` |  |
| deployment.specTemplate.labels | object | `{}` |  |
| httpRoute | object | `{"enabled":false,"filters":[],"hostnames":[],"parentRefs":[],"pathPrefix":"/","referenceGrant":{"enabled":false,"fromNamespace":""}}` | Optional Gateway API HTTPRoute exposing /access through a cluster Gateway. The HTTPRoute and the Service must live in the same namespace. Users supply the Gateway themselves (shared or dedicated). |
| httpRoute.filters | list | `[]` | Optional filters (e.g. URLRewrite, RequestHeaderModifier). |
| httpRoute.hostnames | list | `[]` | Hostnames the route matches. Leave empty to match any host on the selected Gateway listener. |
| httpRoute.parentRefs | list | `[]` | Gateways the route attaches to. |
| httpRoute.pathPrefix | string | `"/"` | Path prefix matched by the route. "/" catches all paths; set to e.g. "/pm" to share a Gateway with other services. The server honors any path prefix in --path-prefix, so no URL rewrite is needed. |
| httpRoute.referenceGrant | object | `{"enabled":false,"fromNamespace":""}` | Optional ReferenceGrant permitting HTTPRoutes from another namespace to backendRef this chart's Service. Only needed when the HTTPRoute is created in a different namespace than this release. |
| image.digest | string | `""` | Image digest (when set, overrides tag: registry/repository@digest). |
| image.registry | string | `"ghcr.io"` | Image registry. |
| image.repository | string | `"platform-mesh/platform-mesh/platform-mesh-sdk-access"` | Image repository (without registry). |
| ingress | object | `{"annotations":{},"className":"","enabled":false,"hosts":[],"tls":[]}` | Optional classic Ingress exposing /access, for clusters without Gateway API. The Ingress and the Service must live in the same namespace. |
| pathPrefix | string | `""` | Optional URL path prefix under which /versions and /vN/access are mounted. Leave empty for root. Use when fronting the server with an ingress that preserves a prefix on the request path (e.g. "/platform-mesh" to serve at https://host/platform-mesh/{versions,v1/access}). |
| security.mountServiceAccountToken | bool | `true` |  |
| service.nodePort | string | `""` | Static NodePort to pin when service.type is NodePort. Leave unset to let Kubernetes assign one from the service-node-port-range. |
| service.type | string | `"ClusterIP"` | Service type exposing /access to access-operator. ClusterIP works for in-cluster deployments; LoadBalancer / NodePort for cross-cluster reachability. |
| tls.dnsNames | list | `["platform-mesh-access.platform-mesh-system.svc"]` | DNS names covered by the TLS cert. At minimum the service DNS name should be here; add external hostnames (if ingress is used) to the list. |
| tls.enabled | bool | `true` | Serve /access over TLS at the Pod. Set to false when TLS termination happens upstream (ingress / gateway) and the Pod should listen on plain HTTP. When false, no Certificate is issued and no cert volume is mounted. |
| tls.ipAddresses | list | `[]` | IP addresses covered by the TLS cert. |
| tls.issuerRef | string | `""` | cert-manager ClusterIssuer used to issue the Certificate. Leave empty to skip Certificate creation (user mounts an externally managed Secret under the configured secretName). |
| tls.secretName | string | `"platform-mesh-access-tls"` | Name of the cert-manager Certificate / resulting Secret holding the TLS cert+key for /access. Created by the chart below. |

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
# platform-mesh-sdk-access-server

![Version: 0.0.0](https://img.shields.io/badge/Version-0.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.0.0](https://img.shields.io/badge/AppVersion-0.0.0-informational?style=flat-square)

A Helm chart to deploy the Platform Mesh access SDK HTTP server on a user-side cluster.

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| file://../common | common | 0.0.0 |
| file://../platform-mesh-sdk-access-crds | platform-mesh-sdk-access-crds | 0.0.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| accessPort | int | `8443` | Port the /access listener binds to inside the Pod. Exposed externally via the Service below. |
| crds.enabled | bool | `false` |  |
| deployment.replicas | int | `1` |  |
| deployment.specTemplate.annotations | object | `{}` |  |
| deployment.specTemplate.labels | object | `{}` |  |
| httpRoute | object | `{"enabled":false,"filters":[],"hostnames":[],"parentRefs":[],"pathPrefix":"/","referenceGrant":{"enabled":false,"fromNamespace":""}}` | Optional Gateway API HTTPRoute exposing /access through a cluster Gateway. The HTTPRoute and the Service must live in the same namespace. Users supply the Gateway themselves (shared or dedicated). |
| httpRoute.filters | list | `[]` | Optional filters (e.g. URLRewrite, RequestHeaderModifier). |
| httpRoute.hostnames | list | `[]` | Hostnames the route matches. Leave empty to match any host on the selected Gateway listener. |
| httpRoute.parentRefs | list | `[]` | Gateways the route attaches to. |
| httpRoute.pathPrefix | string | `"/"` | Path prefix matched by the route. "/" catches all paths; set to e.g. "/pm" to share a Gateway with other services. The server honors any path prefix in --path-prefix, so no URL rewrite is needed. |
| httpRoute.referenceGrant | object | `{"enabled":false,"fromNamespace":""}` | Optional ReferenceGrant permitting HTTPRoutes from another namespace to backendRef this chart's Service. Only needed when the HTTPRoute is created in a different namespace than this release. |
| image.digest | string | `""` | Image digest (when set, overrides tag: registry/repository@digest). |
| image.registry | string | `"ghcr.io"` | Image registry. |
| image.repository | string | `"platform-mesh/platform-mesh/platform-mesh-sdk-access"` | Image repository (without registry). |
| ingress | object | `{"annotations":{},"className":"","enabled":false,"hosts":[],"tls":[]}` | Optional classic Ingress exposing /access, for clusters without Gateway API. The Ingress and the Service must live in the same namespace. |
| pathPrefix | string | `""` | Optional URL path prefix under which /versions and /vN/access are mounted. Leave empty for root. Use when fronting the server with an ingress that preserves a prefix on the request path (e.g. "/platform-mesh" to serve at https://host/platform-mesh/{versions,v1/access}). |
| security.mountServiceAccountToken | bool | `true` |  |
| service.nodePort | string | `""` | Static NodePort to pin when service.type is NodePort. Leave unset to let Kubernetes assign one from the service-node-port-range. |
| service.type | string | `"ClusterIP"` | Service type exposing /access to access-operator. ClusterIP works for in-cluster deployments; LoadBalancer / NodePort for cross-cluster reachability. |
| tls.dnsNames | list | `["platform-mesh-access.platform-mesh-system.svc"]` | DNS names covered by the TLS cert. At minimum the service DNS name should be here; add external hostnames (if ingress is used) to the list. |
| tls.enabled | bool | `true` | Serve /access over TLS at the Pod. Set to false when TLS termination happens upstream (ingress / gateway) and the Pod should listen on plain HTTP. When false, no Certificate is issued and no cert volume is mounted. |
| tls.ipAddresses | list | `[]` | IP addresses covered by the TLS cert. |
| tls.issuerRef | string | `""` | cert-manager ClusterIssuer used to issue the Certificate. Leave empty to skip Certificate creation (user mounts an externally managed Secret under the configured secretName). |
| tls.secretName | string | `"platform-mesh-access-tls"` | Name of the cert-manager Certificate / resulting Secret holding the TLS cert+key for /access. Created by the chart below. |

