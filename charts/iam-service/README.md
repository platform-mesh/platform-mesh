# iam-service

A Helm chart for Kubernetes

## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| caSecret | string | `"domain-certificate"` |  |
| exposure.baseDomain | string | `""` | Public hostname without protocol or port. Required for public resources; the platform-mesh operator supplies it from PlatformMesh.spec.exposure. |
| exposure.normalizedPort | string | `""` | URL port string derived by the platform-mesh operator from protocol and port: empty for HTTP/80 or HTTPS/443, otherwise ":<port>". |
| exposure.port | int | `443` | Public port. |
| exposure.protocol | string | `"https"` |  |
| gatewayApi.httpRoute.filters | list | `[{"type":"URLRewrite","urlRewrite":{"path":{"replacePrefixMatch":"/graphql","type":"ReplacePrefixMatch"}}}]` | list of HTTPRoute filters (default: URLRewrite only, no CORS) |
| gatewayApi.httpRoute.hostnames | list | `[]` | Explicit route hostnames; empty uses exposure.baseDomain and its wildcard. |
| gatewayApi.httpRoute.parentRefs[0].name | string | `"k8sapi-gateway"` |  |
| gatewayApi.httpRoute.pathPrefix | string | `"/iam/graphql"` |  |
| health.port | int | `8080` |  |
| hostAliases.enabled | bool | `false` |  |
| idm.provider | string | `"keycloak"` | Identity provider integration: keycloak, or none to run without a user directory. With none the keycloak values are ignored and the client secret is not needed. |
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/platform-mesh/iam-service"` | The image repository path (without registry) |
| jwt.userIdClaim | string | `"email"` |  |
| kcp.kubeconfig.secretName | string | `"iam-service-kubeconfig"` |  |
| keycloak.baseUrl | string | `""` | Explicit Keycloak base URL for an external identity provider; empty derives the public URL plus /keycloak when exposure.baseDomain is set. Required without a base domain. |
| keycloak.client.id | string | `"iam-service"` |  |
| keycloak.client.secret.key | string | `"client_secret"` |  |
| keycloak.client.secret.name | string | `"iam-client-secret"` |  |
| log.noJson | bool | `true` | Disable JSON log formatting |
| openfga.endpoint | string | `"openfga.platform-mesh-system.svc.cluster.local:8081"` |  |
| port | int | `8080` |  |
| roles.raw.roles[0].groupResource | string | `"core.platform-mesh.io/Account"` |  |
| roles.raw.roles[0].roles[0].description | string | `"Full access to all resources within the account."` |  |
| roles.raw.roles[0].roles[0].displayName | string | `"Owner"` |  |
| roles.raw.roles[0].roles[0].id | string | `"owner"` |  |
| roles.raw.roles[0].roles[1].description | string | `"Limited access to resources within the account. Can view and interact with resources but cannot administrate them."` |  |
| roles.raw.roles[0].roles[1].displayName | string | `"Member"` |  |
| roles.raw.roles[0].roles[1].id | string | `"member"` |  |
| roles.raw.roles[1].groupResource | string | `"Namespace"` |  |
| roles.raw.roles[1].roles[0].description | string | `"Full access to all resources within the account."` |  |
| roles.raw.roles[1].roles[0].displayName | string | `"Owner"` |  |
| roles.raw.roles[1].roles[0].id | string | `"owner"` |  |
| roles.raw.roles[1].roles[1].description | string | `"Limited access to resources within the account. Can view and interact with resources but cannot administrate them."` |  |
| roles.raw.roles[1].roles[1].displayName | string | `"Member"` |  |
| roles.raw.roles[1].roles[1].id | string | `"member"` |  |

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
