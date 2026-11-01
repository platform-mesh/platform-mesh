# Migration guide for update to release-0.2.0

This guide describes the in-place migration procedure from release-0.1.1 to release-0.2.0.

For backup and restore procedures, see the [Backup and Restore Guide](backup-restore.md).

---

## 1. Version Matrix

| Component | Source Version | Target Version | Notes |
|-----------|----------------|----------------|-------|
| **Core Control Plane** | | | |
| platform-mesh-operator | v0.26.3 | v0.47.5 | minor version bump but actually a major KCP configuration change |
| KCP | v0.29.0 | v0.30.0 | minor version update |
| etcd | v0.6.0 | v0.6.0 | |
| etcd-druid | v0.31.0 | v0.34.0 | operator minor version update |
| **Identity & Authorization** | | | |
| Keycloak | 26.3.3-debian-12-r0 | 26.5.2-debian-12-r0 | Helm chart minor version update |
| OpenFGA | v1.9.0 | v1.11.2 | Helm chart patch version update 0.2.38 -> 0.2.51 |
| **Databases** | | | |
| PostgreSQL (Keycloak) | 17.6.0-debian-12-r4 | 17.6.0-debian-12-r4 | |
| PostgreSQL (OpenFGA) | 15.4.0-debian-11-r45 | 17.6.0-debian-12-r4 | major update |
| **Supporting Infrastructure** | | | |
| cert-manager | v1.19.1 | v1.19.2 | patch update |
| Traefik | v3.6.0 | v3.6.7 | patch update |
| Gateway API | v1.4.0 | v1.4.1 | patch update |

---

## 2. Pre-migration

Before starting the migration, create a full backup of all data stores by following the [Backup and Restore Guide](backup-restore.md#backup).

---

## 3. Migration procedure

### 3.1 Remove existing Platform-Mesh resources

Delete the PlatformMesh custom resource, the ResourceGraphDefinition, and the operator:

```shell
kubectl -n platform-mesh-system delete PlatformMesh platform-mesh
kubectl --namespace default delete resourcegraphdefinition platform-mesh-operator
kubectl -n default delete platformmeshoperator platform-mesh-operator
```

### 3.2 Remove HelmReleases

Suspend, delete, and clean up all Platform-Mesh HelmReleases:

```shell
flux suspend helmrelease platform-mesh-operator-components -n default
kubectl -n default delete HelmRelease infra
flux resume helmrelease platform-mesh-operator-components -n default
```

Delete the remaining HelmReleases. You may need to manually remove their finalizers:

```shell
kubectl -n default delete HelmRelease platform-mesh-operator-components
kubectl -n default delete HelmRelease platform-mesh-operator-infra-components
kubectl -n default delete HelmRelease default-idp
```

> **Note:** If deletion hangs, manually remove the finalizers from the `platform-mesh-operator-components`, `infra`, and `default-idp` HelmRelease resources.

Delete the stale Helm release secrets and kcp kubeconfig secrets:

```shell
kubectl delete secrets -n default sh.helm.release.v1.default-idp.v1 sh.helm.release.v1.infra.v1
```

### 3.3 Remove FluxCD

```shell
helm uninstall -n flux-system flux
```

### 3.4 Deploy release-0.2.0

Deploy the release-0.2.0 version of the platform-mesh stack using the standard installation procedure for your environment.

### 3.5 Restore Keycloak database credentials

Restore the PostgreSQL passwords in the `keycloak-postgresql-keycloak` secret from the backup and restart Keycloak so it picks up the restored credentials:

```shell
kubectl -n platform-mesh-system patch secret keycloak-postgresql-keycloak \
  -p "$(yq '{"data": .data}' backup/keycloak/postgres/keycloak-postgresql-keycloak.yaml -o json)"
kubectl -n platform-mesh-system delete pod keycloak-0
```

### 3.6 Restore OpenFGA database

Restore the OpenFGA PostgreSQL database from the backup. See the [Restore OpenFGA](backup-restore.md#restore-openfga) section for details.

### 3.7 Fix the APIExport

The operator may fail to update the APIExport in `root:platform-mesh-system`. Delete it and restart the operator so it gets recreated:

```shell
docs/migration-0.2.0/fix_apiexport.sh
```

### 3.8 Patch APIBindings

Fix stale APIBindings across workspaces:

```shell
KUBECONFIG_PATH=<kcp-admin-kubeconfig> docs/migration-0.2.0/fix_apibindings.sh
```

### 3.9 Create the IdentityProviderConfiguration resource

Create the IDP resource in all org workspaces. Adjust the redirect URIs and secret references to match your environment:

```shell
KUBECONFIG_KCP=<kcp-admin-kubeconfig> PORTAL_HOST=<portal-host> docs/migration-0.2.0/create_identityprovider_config.sh
```

For a dry run to preview the changes:

```shell
KUBECONFIG_KCP=<kcp-admin-kubeconfig> PORTAL_HOST=<portal-host> docs/migration-0.2.0/create_identityprovider_config.sh --dry-run
```

For more options:

```shell
docs/migration-0.2.0/create_identityprovider_config.sh --help
```

### 3.10 Invite organization owners

Create Invite resources for organization owners in their respective org workspaces. The script looks up the creator email from the Account resources and creates Invites:

```shell
KUBECONFIG_KCP=<kcp-admin-kubeconfig> docs/migration-0.2.0/invite_org_owners.sh
```

For a dry run to preview the invites:

```shell
KUBECONFIG_KCP=<kcp-admin-kubeconfig> docs/migration-0.2.0/invite_org_owners.sh --dry-run
```

For more options:

```shell
docs/migration-0.2.0/invite_org_owners.sh --help
```

### 3.11 Reconcile Keycloak clients

Delete the `default` and `kubectl` clients from user-created realms in Keycloak, then restart the security-operator pods so they re-create them with the updated configuration:

```shell
docs/migration-0.2.0/reconcile_keycloak_clients.sh
```

### 3.12 Patch AccountInfo OIDC fields

Update the `spec.oidc` field and client IDs on AccountInfo objects to match the ones currently in Keycloak:

```shell
docs/migration-0.2.0/fix_accountinfo.sh
```

### 3.13 Update FGA store models

Edit the user-created store FGA models to match the one from the `security-operator` chart and clear stale `authorizationModelId` from their status:

```shell
docs/migration-0.2.0/fix_stores.sh
```

### 3.14 Fix WorkspaceAuthenticationConfiguration

```shell
docs/migration-0.2.0/fix_workspaceauthenticationconfiguration.sh
```

! NOTE: Make sure the `orgs-authentication` WorkspaceAuthenticationConfiguration resource is also updated in the `:root` workspace!

### 3.15 Restart security-operator

```shell
kubectl -n platform-mesh-system delete pod -l app=security-operator
kubectl -n platform-mesh-system delete pod -l service=security-operator-generator
kubectl -n platform-mesh-system delete pod -l service=security-operator-initializer
```

### 3.17 Final deployment pass

Re-run the deployment procedure for your environment and wait for the PlatformMesh resource to report a ready status.

---

## 4. Post-upgrade checks

- PlatformMesh resource status is OK
- Gateway, HTTPRoutes, and TLSRoutes resources are in ready state
- security-operator pods have no errors in logs
- Users can login, navigate to accounts and HttpBin resources
- Users can onboard new organisations, accounts, and managed resources
- Browser GraphQL requests to the gateway return no errors
- New FGA stores for onboarded organisations contain 27 (not 22) types
- Store, Account resources in kcp are READY
- For additional checks run `docs/validate_kcp_resources.sh`
- check logs in security-operator
- check if accounts and members UIs in the portal are working

---

## 5. Troubleshooting

### HelmRelease stuck or not getting ready

**Cause:** Stale Helm state preventing reconciliation.

**Fix:** Delete the stuck HelmRelease and its corresponding Helm secret, then reapply it:

```shell
kubectl -n <namespace> delete HelmRelease <name>
kubectl -n <namespace> delete secret sh.helm.release.v1.<name>.v1
```

### APIExport not updating

**Cause:** The platform-mesh-operator cannot update the APIExport in `root:platform-mesh-system` after the upgrade.

**Fix:** Delete the APIExport and restart the operator (see [step 3.7](#37-fix-the-apiexport)).

### api-syncagent pods crashing

**Cause:** Stale URL in the `APIExportEndpointSlice` resource from the previous installation.

**Fix:** Delete the old APIExportEndpointSlice resource -- it will be automatically recreated:

```shell
KUBECONFIG=<kcp-admin-kubeconfig> kubectl ws :root:platform-mesh-system
KUBECONFIG=<kcp-admin-kubeconfig> kubectl delete apiexportendpointslice <name>
```

### Store resources not ready

**Cause:** The `authorizationModelId` field in a Store resource's status does not match the current model in OpenFGA.

**Fix:** Remove the stale field and let the security-operator reconcile it (see [step 3.13](#313-update-fga-store-models)):

```shell
kubectl patch store <store-name> --type=json \
  -p '[{"op": "remove", "path": "/status/authorizationModelId"}]' --subresource=status
```

### GraphQL gateway returns unauthorized

**Cause:** Incorrect client IDs in the `audiences` field of `WorkspaceAuthenticationConfiguration` resources.

**Fix:** Run `docs/migration-0.2.0/fix_workspaceauthenticationconfiguration.sh` or manually patch the `audiences` to use the correct client IDs from Keycloak (see [step 3.14](#314-fix-workspaceauthenticationconfiguration)).

### Cannot query Accounts via GraphQL

**Cause:** Invalid or stale `ContentConfiguration` resources in the `root:platform-mesh-system` workspace.

**Fix:** Delete the invalid ContentConfigurations and restart the platform-mesh-operator:

```shell
KUBECONFIG=<kcp-admin-kubeconfig> kubectl ws :root:platform-mesh-system
KUBECONFIG=<kcp-admin-kubeconfig> kubectl delete contentconfiguration <name>
kubectl -n platform-mesh-system delete pod -l app=platform-mesh-operator
```

### KCP resources to verify after migration

If issues persist, inspect the following resource types in the relevant kcp workspaces:

- `AccountInfo` -- ensure `spec.oidc` and client IDs are correct
- `Store` -- ensure `status.authorizationModelId` is cleared or matches OpenFGA
- `APIBinding` -- ensure bindings point to the updated APIExport
- `WorkspaceAuthenticationConfiguration` -- ensure `audiences` contain the correct client IDs
- `ContentConfiguration` -- ensure no stale entries with an invalid `VALID` flag exist in `root:platform-mesh-system`

### domain-certificate-ca secret not found by the operator

If it doesn't exist, create the secret using existing orchestration tools.

### Keycloak not sending emails

Missing configuration for realm.


### kcp 'failed to verify' keycloak endpoint

Wrong data in the `domain-certificate-ca` 'tls.key' must actually contain to CA!

### invite users to orgnization

Create Invite resources in the respective org workspace:
```yaml
apiVersion: core.platform-mesh.io/v1alpha1
kind: Invite
metadata:
  name: username
spec:
  email: username@sap.com
```

Then set password, verify email and remove 'Required user actions' in keycloak.

## kubernetes-graphql-gateway Unauthorized responses

Cause: missing or incorrect `audiences` in orgs-authentication WorkspaceAuthenticationConfiguration resource in the ':root' kcp workspace.

## kubernetes-graphql-gateway certificate verifycation issue
https://rebac-authz-webhook.platform-mesh-system.svc.cluster.local:9443/authz?timeout=30s\\\": tls: failed to verify certificate: x509: certificate signed by unknown authority\") has prevented the request from succeeding (get accounts.core.platform-mesh.io

Fix: restart the `kubernetes-graphql-gateway` and `root-kcp` pod

## store resources not ready
## security-operator logs: duplicate type definitions

Symptop: Some APIBindings exist which have errors in status. Should be deleted or fixed, otherwise store reconcile issues remain with duplicate type definitions.

Solution: Resolved by removing duplicate AuthorizationModels and APIResourceSchemas from kcp provider workspaces.

## iam-service UI missing

GraphQL response:
```gql
{
    "errors": [
        {
            "message": "failed to test if action is allowed: failed to check permission with openfga: rpc error: code = Code(2000) desc = relation 'core_platform-mesh_io_account#get_iam_users' not found",
            "path": [
                "users"
            ]
        }
    ],
    "data": null
}
```

Fixes:
- update iam-service version to `v0.13.3`


## add marketplace feature toggles to platform-mesh

```yaml
  featureToggles:
  - name: feature-enable-marketplace-account
  - name: feature-enable-marketplace-org
```

## remove Kustomization which overrides the ContentConfigurations

Issue: externally deployed ContentConfiguration are overriding the ones deployed by the operator.
Symptop: UI issues.

Fix: remove Kustomization resource `ui-configs` from the showroom-mcp chart.

## remove store-tuples which grant access to every logged in user to the org (owner should invite them explicitely)

Previously, all logged-in users were members in any org, due to tuples configured in the `Store` resource.

Fix: remove the tuples from store resources.

After that, all new users must be explicitely added via the Members UI.

## iam-service graphql unauthorized

cause: stale secrets. In kcp's `:root:orgs` the WorkspaceAuthenticationConfiguration orgs-authentication should have '.spec.jwt[0].issuer.certificateAuthority' matching the **domain-certificate** ca.crt field. It usually get's it from **domain-certificate-ca** tls.crt. Those must be up-to-date. With newer versions of the operator this will be configurable not not neceserrily to sync.