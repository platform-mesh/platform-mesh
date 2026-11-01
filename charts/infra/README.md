# infra

Infrastructure dependencies for a Platform Mesh installation (kcp, Keycloak, cert-manager, etc.). This chart is not intended to be installed directly — it is always managed by the platform-mesh-operator via its profile system, which supplies all required values overrides.

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| cnpg.cluster.image.digest | string | `""` | PostgreSQL image digest (when set, overrides tag) |
| cnpg.cluster.image.registry | string | `"ghcr.io"` | PostgreSQL image registry |
| cnpg.cluster.image.repository | string | `"cloudnative-pg/postgresql"` | PostgreSQL image repository (without registry) |
| cnpg.cluster.image.tag | string | `"18.3"` | PostgreSQL image tag |
| cnpg.cluster.instances | int | `2` |  |
| cnpg.cluster.name | string | `"platform-mesh-pg"` |  |
| cnpg.cluster.postgresql.maxConnections | string | `"200"` |  |
| cnpg.cluster.postgresql.sharedBuffers | string | `"128MB"` |  |
| cnpg.cluster.storage.size | string | `"2Gi"` |  |
| cnpg.databases[0].name | string | `"keycloak"` |  |
| cnpg.databases[0].owner | string | `"keycloak"` |  |
| cnpg.databases[1].name | string | `"openfga"` |  |
| cnpg.databases[1].owner | string | `"openfga"` |  |
| cnpg.enabled | bool | `false` |  |
| cnpg.roles.keycloak.password | string | `""` | REQUIRED: set to a strong random value. |
| cnpg.roles.keycloak.username | string | `"keycloak"` | PostgreSQL username for Keycloak. Written to the cnpg-keycloak-user secret. |
| cnpg.roles.openfga.password | string | `""` | REQUIRED: set to a strong random value. |
| cnpg.roles.openfga.username | string | `"openfga"` | PostgreSQL username for OpenFGA. Written to the cnpg-openfga-user secret. |
| dex.enabled | bool | `false` | Enable Dex as a local upstream OIDC identity provider |
| dex.gatewayApi.filters | list | `[]` | list of HTTPRoute filters (default: none) |
| dex.gatewayApi.hostnames | list | `[]` | hostnames for the Dex HTTPRoute |
| dex.gatewayApi.pathPrefix | string | `"/dex"` | path prefix for the Dex HTTPRoute |
| dex.health.liveness.failureThreshold | int | `3` |  |
| dex.health.liveness.path | string | `"/dex/healthz"` |  |
| dex.health.periodSeconds | int | `10` |  |
| dex.health.readiness.initialDelaySeconds | int | `0` |  |
| dex.health.readiness.path | string | `"/dex/healthz"` |  |
| dex.health.readiness.periodSeconds | int | `10` |  |
| dex.image.digest | string | `"sha256:1b4a6eee8550240b0faedad04d984ca939513650e1d9bd423502c67355e3822f"` | Dex image digest (when set, overrides tag: registry/repository@digest) |
| dex.image.registry | string | `"ghcr.io"` | Dex image registry |
| dex.image.repository | string | `"dexidp/dex"` | Dex image repository (without registry) |
| dex.image.tag | string | `"v2.42.0"` | Dex image tag |
| dex.issuer | string | `""` | Dex issuer URL (must match external browser-reachable URL) |
| dex.resources.limits.cpu | string | `"200m"` |  |
| dex.resources.limits.memory | string | `"256Mi"` |  |
| dex.resources.requests.cpu | string | `"50m"` |  |
| dex.resources.requests.memory | string | `"64Mi"` |  |
| dex.service.name | string | `"dex"` | Dex service name |
| dex.service.port | int | `5556` | Dex service port |
| dex.staticClient.id | string | `"keycloak-broker"` | OIDC client ID for Keycloak identity broker |
| dex.staticClient.name | string | `"Keycloak Broker"` | Display name for the static OIDC client |
| dex.staticClient.redirectURIs | list | `[]` | Allowed redirect URIs (Keycloak broker callback URLs) |
| dex.staticClient.secret | string | `"local-dev-broker-secret"` | OIDC client secret for Keycloak identity broker (local dev only) |
| dex.staticPasswords[0] | object | `{"email":"dex@portal.localhost","hash":"$2a$10$mhivFdR/0pGuCR85LuEerOcYhCuiU294J3dHbtSU3UKDHLNQ6gNHm","userID":"08a868f8-4b4b-42f3-9d23-5f46eda2c3a0","username":"dex"}` | Local test user for Dex password connector (dex@portal.localhost/dex/dex) |
| externalSecrets.enabled | bool | `false` |  |
| gatewayApi.enabled | bool | `true` | Toggle to enable/disable Gateway API resources |
| gatewayApi.gatewayClassName | string | `"traefik"` | GatewayClass name |
| gatewayApi.infrastructure | object | `{}` | HTTPS Terminate configuration |
| gatewayApi.listeners | list | `[{"name":"terminate","port":443,"protocol":"HTTPS","tls":{"certificateRefs":[{"group":"","kind":"Secret","name":"domain-certificate","namespace":"platform-mesh-system"}],"mode":"Terminate"}}]` | Gateway listeners. The HTTPS listener is hostname-less so all route hostnames share one listener (needed for HTTP/2 connection reuse on Istio). |
| gatewayApi.listenersExtra | list | `[{"allowedRoutes":{"namespaces":{"from":"All"}},"hostname":"","name":"terminate-services","port":443,"protocol":"HTTPS","tls":{"certificateRefs":[{"group":"","kind":"Secret","name":"domain-certificate","namespace":"platform-mesh-system"}],"mode":"Terminate"}}]` | Additional listeners to be added to the Gateway resource (e.g. for HTTPBin) |
| gatewayApi.listenersExtra[0].hostname | string | `""` | hostname should match the pattern "*.services.{{ .baseDomain }}" |
| gatewayApi.name | string | `"k8sapi-gateway"` | Name of the Gateway resource |
| gatewayApi.passthrough.allowedRoutes | object | `{"namespaces":{"from":"All"}}` | allowedRoutes of the generated passthrough listeners |
| gatewayApi.passthrough.enabled | bool | `true` | Generate one TLS passthrough listener per kcp hostname (front proxy, root shard, shards). Disable to manage passthrough listeners yourself; kcp TLSRoutes then attach to a listener named "passthrough". |
| gatewayApi.passthrough.port | int | `443` | Port of the generated passthrough listeners |
| hostAliases.enabled | bool | `false` |  |
| kcp.auth.adminCert.enabled | bool | `true` |  |
| kcp.auth.adminCert.privateKey.algorithm | string | `"RSA"` |  |
| kcp.auth.adminCert.privateKey.size | int | `2048` |  |
| kcp.auth.adminCert.subject.organizations[0] | string | `"system:kcp:admin"` |  |
| kcp.auth.oidc.caFileRef.key | string | `"tls.crt"` |  |
| kcp.auth.oidc.caFileRef.name | string | `"domain-certificate-ca"` |  |
| kcp.auth.oidc.clientID | string | `""` |  |
| kcp.auth.oidc.enabled | bool | `false` |  |
| kcp.auth.oidc.groupsClaim | string | `"groups"` |  |
| kcp.auth.oidc.issuerUrl | string | `""` |  |
| kcp.auth.oidc.usernameClaim | string | `"email"` |  |
| kcp.auth.serviceAccount.enabled | bool | `true` |  |
| kcp.cacheServer.etcd.name | string | `"etcd-cache"` |  |
| kcp.cacheServer.etcd.service.name | string | `"etcd-cache-client"` |  |
| kcp.cacheServer.etcd.service.port | int | `2379` |  |
| kcp.cacheServer.name | string | `"cache-server"` |  |
| kcp.certificates.caSecretRef | object | `{}` | Existing CA to sign from directly. Secret must be of type kubernetes.io/tls and contain the CA certificate and its private key. Mutually exclusive with issuerRef. |
| kcp.certificates.issuerRef | object | `{}` | Existing Issuer/ClusterIssuer that signs the kcp certificate chain. It must be able to issue CA certificates. Mutually exclusive with caSecretRef. |
| kcp.etcd.backup.compression.enabled | bool | `false` |  |
| kcp.etcd.backup.compression.policy | string | `"gzip"` |  |
| kcp.etcd.backup.deltaSnapshotMemoryLimit | string | `"1Gi"` |  |
| kcp.etcd.backup.deltaSnapshotPeriod | string | `"300s"` |  |
| kcp.etcd.backup.fullSnapshotSchedule | string | `"0 */24 * * *"` |  |
| kcp.etcd.backup.garbageCollectionPeriod | string | `"43200s"` |  |
| kcp.etcd.backup.garbageCollectionPolicy | string | `"Exponential"` |  |
| kcp.etcd.backup.image.repository | string | `""` |  |
| kcp.etcd.backup.image.tag | string | `""` |  |
| kcp.etcd.backup.leaderElection.etcdConnectionTimeout | string | `"5s"` |  |
| kcp.etcd.backup.leaderElection.reelectionPeriod | string | `"5s"` |  |
| kcp.etcd.backup.port | int | `8080` |  |
| kcp.etcd.backup.resources.limits.cpu | string | `"200m"` |  |
| kcp.etcd.backup.resources.limits.memory | string | `"1Gi"` |  |
| kcp.etcd.backup.resources.requests.cpu | string | `"23m"` |  |
| kcp.etcd.backup.resources.requests.memory | string | `"128Mi"` |  |
| kcp.etcd.defragmentationSchedule | string | `"0 */24 * * *"` |  |
| kcp.etcd.image.repository | string | `""` |  |
| kcp.etcd.image.tag | string | `""` |  |
| kcp.etcd.name | string | `"etcd-kcp"` |  |
| kcp.etcd.quota | string | `"8Gi"` |  |
| kcp.etcd.replicas | int | `1` |  |
| kcp.etcd.resources.limits.cpu | string | `"500m"` |  |
| kcp.etcd.resources.limits.memory | string | `"1Gi"` |  |
| kcp.etcd.resources.requests.cpu | string | `"100m"` |  |
| kcp.etcd.resources.requests.memory | string | `"200Mi"` |  |
| kcp.etcd.serverPort | int | `2380` |  |
| kcp.etcd.service.name | string | `"etcd-kcp-client"` |  |
| kcp.etcd.service.port | int | `2379` |  |
| kcp.etcd.sharedConfig.autoCompactionMode | string | `"periodic"` |  |
| kcp.etcd.sharedConfig.autoCompactionRetention | string | `"30m"` |  |
| kcp.external.hostname | string | `""` | External hostname for kcp. Required — must be set via profile map or PlatformMesh resource (e.g. kcp.api.example.com). |
| kcp.external.port | int | `8443` |  |
| kcp.frontProxy.additionalPathMappings[0].backend | string | `"https://virtual-workspaces.platform-mesh-system:8443"` |  |
| kcp.frontProxy.additionalPathMappings[0].backend_server_ca | string | `"/etc/kcp/tls/ca/tls.crt"` |  |
| kcp.frontProxy.additionalPathMappings[0].path | string | `"/services/contentconfigurations"` |  |
| kcp.frontProxy.additionalPathMappings[0].proxy_client_cert | string | `"/etc/kcp-front-proxy/requestheader-client/tls.crt"` |  |
| kcp.frontProxy.additionalPathMappings[0].proxy_client_key | string | `"/etc/kcp-front-proxy/requestheader-client/tls.key"` |  |
| kcp.frontProxy.additionalPathMappings[1].backend | string | `"https://virtual-workspaces.platform-mesh-system:8443"` |  |
| kcp.frontProxy.additionalPathMappings[1].backend_server_ca | string | `"/etc/kcp/tls/ca/tls.crt"` |  |
| kcp.frontProxy.additionalPathMappings[1].path | string | `"/services/marketplace"` |  |
| kcp.frontProxy.additionalPathMappings[1].proxy_client_cert | string | `"/etc/kcp-front-proxy/requestheader-client/tls.crt"` |  |
| kcp.frontProxy.additionalPathMappings[1].proxy_client_key | string | `"/etc/kcp-front-proxy/requestheader-client/tls.key"` |  |
| kcp.frontProxy.clusterIP | string | `""` |  |
| kcp.frontProxy.extraArgs[0] | string | `"--feature-gates=WorkspaceAuthentication=true"` |  |
| kcp.frontProxy.name | string | `"frontproxy"` |  |
| kcp.frontProxy.replicas | int | `1` |  |
| kcp.frontProxy.resources | object | `{}` | Optional resource requests and limits for the front proxy |
| kcp.image.repository | string | `""` |  |
| kcp.image.tag | string | `""` |  |
| kcp.namespace | string | `"platform-mesh-system"` |  |
| kcp.rootShard.extraArgs[0] | string | `"--feature-gates=WorkspaceAuthentication=true,CacheAPIs=true"` |  |
| kcp.rootShard.hostname | string | `""` | Hostname for the root shard. Defaults to "root.kcp.<kcp.external.hostname>" when unset. |
| kcp.rootShard.replicas | int | `1` |  |
| kcp.rootShard.resources | object | `{}` | Optional resource requests and limits for the root shard |
| kcp.rootShard.shardBaseURL | string | `""` | Base URL the root shard advertises. Defaults to "https://<rootShard.hostname>:<kcp.external.port>/" when unset. |
| kcp.shards[0].name | string | `"nereus"` |  |
| kcp.shards[1].name | string | `"triton"` |  |
| kcp.webhook.authorizationWebhookSecretName | string | `"kcp-webhook-secret"` |  |
| kcp.webhook.caData | string | `""` |  |
| kcp.webhook.enabled | bool | `true` |  |
| kcp.webhook.port | int | `9443` |  |
| kcp.webhook.server | string | `"https://rebac-authz-webhook.platform-mesh-system.svc.cluster.local:9443/authz"` |  |
| kcp.webhook.version | string | `"v1"` |  |
| keycloak.domain | object | `{"name":"platform-mesh.io","pathPrefix":"/keycloak"}` | domain configuration |
| keycloak.domain.name | string | `"platform-mesh.io"` | domain name |
| keycloak.domain.pathPrefix | string | `"/keycloak"` | path prefix |
| keycloak.gatewayApi.filters | list | `[]` | list of HTTPRoute filters (default: none) |
| keycloak.gatewayApi.hostnames | list | `[]` | hostnames for the Keycloak HTTPRoute |
| keycloak.gatewayApi.pathPrefix | string | `"/keycloak"` | path prefix for the Keycloak HTTPRoute |
| keycloak.keycloakConfig.admin | object | `{"password":{"valueFrom":{"secretKeyRef":{"key":"secret","name":"keycloak-admin"}}},"username":{"value":"keycloak-admin"}}` | admin user configuration |
| keycloak.keycloakConfig.admin.password | object | `{"valueFrom":{"secretKeyRef":{"key":"secret","name":"keycloak-admin"}}}` | admin password |
| keycloak.keycloakConfig.admin.password.valueFrom.secretKeyRef.key | string | `"secret"` | key of the password in the secret |
| keycloak.keycloakConfig.admin.password.valueFrom.secretKeyRef.name | string | `"keycloak-admin"` | name of the secret containing the password |
| keycloak.keycloakConfig.admin.username.value | string | `"keycloak-admin"` | username |
| keycloak.keycloakConfig.client | object | `{"name":"welcome","targetSecret":{"name":"portal-client-secret-welcome","namespace":"platform-mesh-system"},"tokenLifespan":3600}` | client configuration |
| keycloak.keycloakConfig.client.name | string | `"welcome"` | client name |
| keycloak.keycloakConfig.client.targetSecret | object | `{"name":"portal-client-secret-welcome","namespace":"platform-mesh-system"}` | target secret options |
| keycloak.keycloakConfig.client.targetSecret.name | string | `"portal-client-secret-welcome"` | secret name |
| keycloak.keycloakConfig.client.targetSecret.namespace | string | `"platform-mesh-system"` | secret namespace |
| keycloak.keycloakConfig.client.tokenLifespan | int | `3600` | token lifespan |
| keycloak.keycloakConfig.realm | object | `{"name":"master"}` | realm configuration |
| keycloak.keycloakConfig.realm.name | string | `"master"` | realm name |
| keycloak.keycloakConfig.redirectUrls | list | `[]` | redirect urls |
| keycloak.keycloakConfig.url | string | `"http://keycloak.platform-mesh-system.svc.cluster.local/keycloak"` | url of the keycloak server |
| keycloak.keycloakConfig.userRegistration.enabled | bool | `true` | toggle to enable/disable user registration |
| keycloak.operator.admin.password | string | `""` | Bootstrap admin password (written to keycloak-admin secret). REQUIRED: set to a strong random value. |
| keycloak.operator.admin.secret | string | `""` | Bootstrap admin client secret (written to keycloak-admin secret). REQUIRED: set to a strong random value. |
| keycloak.operator.admin.username | string | `"keycloak-admin"` | Bootstrap admin username (written to keycloak-admin secret) |
| keycloak.operator.caSecret | string | `""` | Secret with PEM CA for outbound HTTPS trust (e.g. local mkcert CA) |
| keycloak.operator.db.database | string | `"keycloak"` | Database name |
| keycloak.operator.db.host | string | `"platform-mesh-pg-rw.platform-mesh-system.svc.cluster.local"` | PostgreSQL host |
| keycloak.operator.db.password | string | `""` | Database password (written to keycloak-db-credentials secret). REQUIRED: set to a strong random value. |
| keycloak.operator.db.port | int | `5432` | PostgreSQL port |
| keycloak.operator.db.username | string | `"keycloak"` | Database username (written to keycloak-db-credentials secret) |
| keycloak.operator.enabled | bool | `false` | Enable Keycloak CR and supporting secrets managed by Keycloak Operator |
| keycloak.operator.health.liveness.failureThreshold | int | `3` |  |
| keycloak.operator.health.liveness.path | string | `"/keycloak/health/live"` |  |
| keycloak.operator.health.periodSeconds | int | `10` |  |
| keycloak.operator.health.port | int | `9000` |  |
| keycloak.operator.health.readiness.initialDelaySeconds | int | `0` |  |
| keycloak.operator.health.readiness.path | string | `"/keycloak/health/ready"` |  |
| keycloak.operator.health.readiness.periodSeconds | int | `10` |  |
| keycloak.operator.health.startup.failureThreshold | int | `600` |  |
| keycloak.operator.health.startup.path | string | `"/keycloak/health"` |  |
| keycloak.operator.hostname | string | `""` | Keycloak hostname (used in the Keycloak CR hostname.hostname field). Must not include a path — the operator auto-derives http-relative-path from any path component, which conflicts with the value baked into the image. |
| keycloak.operator.httpEnabled | bool | `true` | Enable plain HTTP on the Keycloak pod. Keycloak sits behind gatewayAPI, which handles TLS termination. |
| keycloak.operator.image.digest | string | `""` | Keycloak image digest (when set, overrides tag: registry/repository@digest) |
| keycloak.operator.image.registry | string | `"ghcr.io"` | Keycloak image registry |
| keycloak.operator.image.repository | string | `"platform-mesh/custom-images/keycloak"` | Keycloak image repository (without registry) |
| keycloak.operator.image.tag | string | `"v26.8.1"` | Keycloak image tag (defaults to appVersion) |
| keycloak.operator.instances | int | `1` | Number of Keycloak instances |
| keycloak.operator.resources.limits.cpu | string | `"2"` |  |
| keycloak.operator.resources.limits.memory | string | `"2Gi"` |  |
| keycloak.operator.resources.requests.cpu | string | `"750m"` |  |
| keycloak.operator.resources.requests.memory | string | `"1Gi"` |  |
| keycloak.operator.waitForDb.image.digest | string | `""` | wait-for-db init container image digest (when set, overrides tag) |
| keycloak.operator.waitForDb.image.registry | string | `"docker.io"` | wait-for-db init container image registry |
| keycloak.operator.waitForDb.image.repository | string | `"library/busybox"` | wait-for-db init container image repository (without registry) |
| keycloak.operator.waitForDb.image.tag | string | `"1.37"` | wait-for-db init container image tag |
| keycloak.service | object | `{"name":"keycloak","port":80}` | service configuration |
| keycloak.service.name | string | `"keycloak"` | service name |
| keycloak.service.port | int | `80` | service port |
| openfga.rbac.writePrincipals[0] | string | `"cluster.local/ns/platform-mesh-system/sa/iam-service"` |  |
| openfga.rbac.writePrincipals[1] | string | `"cluster.local/ns/platform-mesh-system/sa/security-operator"` |  |
| openfga.rbac.writePrincipals[2] | string | `"cluster.local/ns/platform-mesh-system/sa/account-operator"` |  |

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
