# Installation credentials

Keycloak generates admin credentials natively. Database password generation is enabled for kind and opt-in elsewhere.

| Secret | Default username | Created by |
| --- | --- | --- |
| keycloak-initial-admin | temp-admin | Keycloak operator |
| cnpg-keycloak-user | keycloak | User or infra bootstrap Job |
| cnpg-openfga-user | openfga | User or infra bootstrap Job |

## Configuration

Supply both database Secrets in the runtime namespace (normally `platform-mesh-system`), with `username` and `password` keys. Usernames must match `cnpg.roles.<role>.username`. CNPG requires these fixed Secret names even when Keycloak uses another database Secret.

Alternatively, set `spec.values.services.infra.values.credentials.enabled: true` on the PlatformMesh resource, or `components.services.infra.values.credentials.enabled: true` in a custom profile. Setting it to `false` disables the Job without deleting credentials or affecting native admin generation.

The Job generates missing database passwords with 256 bits of randomness and preserves existing Secrets, including legacy passwords. Generated Secrets survive uninstall: back them up with PostgreSQL. Deleting Secrets does not rotate database passwords.

Under the infra service values:

- `keycloak.operator.admin.existingSecret` selects supplied admin credentials. Also set the `pm-keycloak-operator` service's `keycloakSecret` and `initContainer.keycloakUser` to match.
- `keycloak.operator.db.existingSecret` selects supplied database credentials; required for external databases unless `db.password` is set.
- Existing Secrets need `username`/`password` keys and cannot be combined with the corresponding password override.
- For explicit shared-database passwords, prefer `cnpg.roles.<role>.password`. If also setting `keycloak.operator.db.password`, keep it equal to the Keycloak role password.

## Existing installations

`pmctl quickstart` preserves credential settings when replacing the profile. Ordinary Helm upgrades retain profiles, so older kind profiles need an explicit generation opt-in.

To migrate legacy credentials:

1. Keep existing password overrides during the first chart upgrade, which adds Secret retention annotations.
2. Enable `credentials.enabled`. For retained profiles using OCM, copy the shipped `credentials-image` entry into `components.services.infra.imageResources`, or configure the helper image directly.
3. After that upgrade, set `keycloak.operator.admin.existingSecret` to the existing admin Secret before removing its password override. Keep the `pm-keycloak-operator` Secret and username settings aligned.
4. Remove database password overrides only after the retention upgrade. Keycloak can read `cnpg-keycloak-user` directly, or retain `keycloak.operator.db.existingSecret: keycloak-db-credentials`. The Job reuses matching legacy credentials and rejects conflicts.

Do not switch an existing database to `keycloak-initial-admin`: changing the bootstrap Secret does not reset its stored admin account.

## Retrieve admin credentials

Use the Secret referenced by `Keycloak.spec.bootstrapAdmin.user.secret` (`keycloak-initial-admin` for fresh installs), or source `contrib/tilt/env.sh` to load `KEYCLOAK_ADMIN_USER` and `KEYCLOAK_ADMIN_PASSWORD`.
