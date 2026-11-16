# Installation credentials

Fresh installations use native Keycloak admin generation and a database-only infra chart Job. PMO passes chart values; it has no credential-generation logic.

| Secret | Created by | Contents |
| --- | --- | --- |
| keycloak-initial-admin | Keycloak operator | username (temp-admin) and random password |
| cnpg-keycloak-user | Infra chart Job | Keycloak database username and password |
| cnpg-openfga-user | Infra chart Job | OpenFGA database username and password |

Keycloak and OpenFGA use the same CNPG cluster with separate database roles. Keycloak reads cnpg-keycloak-user directly on fresh installations. No duplicate database Secret or admin client secret is generated.

CNPG natively generates credentials for one bootstrap application user per cluster; its additional managed roles require supplied password Secrets. The OpenFGA chart consumes credentials for external PostgreSQL without generating them. See [CNPG bootstrap](https://github.com/cloudnative-pg/cloudnative-pg/blob/v1.30.1/docs/src/bootstrap.md), [role password management](https://github.com/cloudnative-pg/cloudnative-pg/blob/v1.30.1/docs/src/declarative_role_management.md#password-management), and [Keycloak admin generation](https://github.com/keycloak/keycloak/blob/26.8.1/operator/src/main/java/org/keycloak/operator/controllers/KeycloakAdminSecretDependentResource.java).

## Database bootstrap and lifecycle

Each database password contains 256 bits of cryptographic randomness, encoded as 64 hexadecimal characters. The Job creates only missing cnpg-keycloak-user and cnpg-openfga-user Secrets, retaining existing credentials, including older weak passwords. It validates both groups before writing; incomplete Secrets, mismatched usernames and conflicting passwords fail without disclosing credentials.

For shared-database upgrades, the Job also reads the legacy keycloak-db-credentials Secret (or a configured db.existingSecret). It reuses a matching legacy password if the CNPG role Secret is missing, but never creates the legacy Secret. Explicit password overrides remain supported and are rendered by ordinary chart templates. Deliberate rotation requires coordination with PostgreSQL and all consumers; removing a Secret is not a rotation procedure.

Helm runs the Job as a pre-install/pre-upgrade hook, Argo CD as PreSync, and Tilt applies it before PostgreSQL. Random passwords are not generated during chart rendering, so repeated offline renders do not rotate credentials. The configuration and script are mounted from a separate Secret; supplied password overrides are also stored there.

The Job can get only the database Secret names and create Secrets within its namespace. It has no list, update, patch, delete or cluster-wide permission. Kubernetes RBAC cannot constrain create by Secret name. It cannot read the Keycloak admin Secret. The pinned helper image uses the common registry/repository/tag/digest schema and is included in the infra OCM inventory.

Generated database Secrets have no owner reference or release tracking, and survive release removal. Back them up with the database. Hook ServiceAccount, Role, RoleBinding and configuration Secret persist between runs and are replaced before each run; remove these helpers separately when retiring an installation.

The native admin Secret belongs to the Keycloak resource and is garbage-collected if that resource is deleted. Keycloak only bootstraps the admin account when the master realm is first created. Recreating its Secret or changing its bootstrap configuration does not reset an account in an existing database. The native account is a temporary bootstrap administrator; replacing or deleting it must also account for consumers that still use it, including the security-operator bootstrap container.

## Existing installations

1. Keep all existing explicit credential overrides during the first chart upgrade. It adds Helm resource-policy: keep and Argo Prune=false annotations to legacy explicit Secrets. This prevents their deletion when templates are subsequently removed.
2. Add the credentials-image entry from the shipped profile to components.services.infra.imageResources when using OCM localization, or configure an available helper image directly. Profile ConfigMaps are installed once and do not automatically receive new entries.
3. After the retention upgrade succeeds, set keycloak.operator.admin.existingSecret to the existing admin Secret, usually keycloak-admin, before removing its password override. Keep security-operator.keycloakSecret and initContainer.keycloakUser pointing to the existing Secret and username. Do not switch an existing database to keycloak-initial-admin: a newly generated password would not match its stored account.
4. Remove database password overrides only after the retention upgrade. Keycloak can use cnpg-keycloak-user directly when its credentials match; alternatively keep db.existingSecret: keycloak-db-credentials. The Job preserves both legacy and current passwords and rejects conflicts before creating Secrets.

Infra settings belong under services.infra.values in PlatformMesh overrides, or components.services.infra.values in a custom profile. Security-operator consumer settings belong under the corresponding security-operator service values. Retained profiles keep their old admin consumer defaults; if replacing a profile with the new shipped one, explicitly restore the existing admin Secret and username settings.

## Retrieval and overrides

For a fresh installation, retrieve credentials from the runtime cluster:

    kubectl -n platform-mesh-system get secret keycloak-initial-admin -o jsonpath='{.data.username}' | base64 -d
    kubectl -n platform-mesh-system get secret keycloak-initial-admin -o jsonpath='{.data.password}' | base64 -d

For existing installations, use the Secret named in the Keycloak resource's spec.bootstrapAdmin.user.secret. Source contrib/tilt/env.sh to load KEYCLOAK_ADMIN_USER and KEYCLOAK_ADMIN_PASSWORD from that configured Secret. Portal tests require the password and have no shared-password fallback.

Set admin.existingSecret to use a pre-provisioned admin Secret with username/password keys. Set db.existingSecret for a pre-provisioned database Secret with the same keys. Existing Secret references and explicit password overrides are mutually exclusive for that credential. An external Keycloak database requires db.existingSecret or an explicit db.password; the Job does not provision external databases.

When using admin.password or admin.existingSecret with a new shipped profile, also override the security-operator service values: keycloakSecret must name that admin Secret and initContainer.keycloakUser must match its username. Native defaults are keycloak-initial-admin and temp-admin; an explicit password defaults to keycloak-admin with username keycloak-admin.

For explicit shared-database passwords, keep keycloak.operator.db.password and cnpg.roles.keycloak.password equal. Supplying only db.password lets the Job create the CNPG role Secret with that value. Supplying only cnpg.roles.keycloak.password lets Keycloak use that role Secret directly. Set credentials.enabled: false when database Secrets are provisioned by an external manager. This does not disable native Keycloak admin generation.

Dex remains disabled in the supplied profiles. Enabling it requires dex.staticClient.secret and matching broker configuration. No default user or shared client secret is supplied; provide users explicitly with bcrypt hashes of strong passwords.
