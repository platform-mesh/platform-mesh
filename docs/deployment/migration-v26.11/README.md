# Migrate OpenFGA to the shared CNPG cluster

See also [keycloak-api-group.md](keycloak-api-group.md) for the move of
`IdentityProviderConfiguration`/`Invite` to `keycloak.platform-mesh.io` in the
same release (automatic).

This procedure moves OpenFGA from its bundled PostgreSQL instance to the shared CNPG cluster. It requires a maintenance window.

Run the commands from the repository root in the same shell. Apply profile changes through the source that owns the profile. The examples below use a directly managed `platform-mesh-profile` ConfigMap.

## 1. Prepare CNPG and save the current profile

Ensure `components.services.infra.values.cnpg` contains the OpenFGA role and database. Preserve the existing credentials if they are already configured.

```yaml
cnpg:
  enabled: true
  roles:
    openfga:
      username: openfga
      password: "<strong-random-password>"
  databases:
    - name: openfga
      owner: openfga
```

After every profile change, request PlatformMesh reconciliation with the function below.

```shell
reconcile() {
  kubectl annotate platformmesh platform-mesh -n platform-mesh-system \
    reconcile.fluxcd.io/requestedAt="$(date -u +%Y-%m-%dT%H:%M:%SZ)" --overwrite
}

reconcile
kubectl wait database/openfga-db -n platform-mesh-system \
  --for=jsonpath='{.status.applied}'=true --timeout=300s

umask 077
BACKUPDIR=backup/openfga-migration
mkdir -p "$BACKUPDIR"
kubectl get configmap platform-mesh-profile -n platform-mesh-system -o yaml \
  > "$BACKUPDIR/profile-before.yaml"
ORIGINAL_REPLICAS=$(kubectl get deployment openfga -n platform-mesh-system \
  -o jsonpath='{.spec.replicas}')
```

Record the original autoscaling settings, store and model IDs, tuple counts, and representative allow/deny checks. Protect the profile backup because it can contain credentials.

## 2. Stop OpenFGA

In `components.services.openfga.values`, set:

```yaml
replicaCount: 0
autoscaling:
  enabled: false
postgresql:
  enabled: true
```

Keep the old datastore settings unchanged. Do not scale the Deployment directly because a controller can restore it.

```shell
cp "$BACKUPDIR/profile-before.yaml" "$BACKUPDIR/profile-maintenance.yaml"
yq -i 'del(.metadata.resourceVersion, .metadata.uid, .metadata.creationTimestamp,
  .metadata.managedFields,
  .metadata.annotations."kubectl.kubernetes.io/last-applied-configuration")' \
  "$BACKUPDIR/profile-maintenance.yaml"

# Edit data.profile.yaml as shown above.
kubectl apply -f "$BACKUPDIR/profile-maintenance.yaml"
reconcile
kubectl wait helmrelease/openfga -n platform-mesh-system \
  --for=jsonpath='{.spec.values.replicaCount}'=0 --timeout=300s
kubectl wait deployment/openfga -n platform-mesh-system \
  --for=jsonpath='{.spec.replicas}'=0 --timeout=300s
kubectl wait pods -n platform-mesh-system \
  -l app.kubernetes.io/name=openfga,app.kubernetes.io/instance=openfga \
  --for=delete --timeout=120s
```

Do not continue until all OpenFGA application pods are gone.

## 3. Back up the source database

```shell
docs/migration-openfga-cnpg/openfga_migrate.sh backup "$BACKUPDIR"
```

Keep the printed `.dump` file, its `.sha256` file, and the original profile together. Leave OpenFGA stopped.

## 4. Restore and compare the databases

Use the filename printed by the backup command.

```shell
BACKUPFILE="$BACKUPDIR/backup-<timestamp>-<unique-id>.dump"
docs/migration-openfga-cnpg/openfga_migrate.sh restore "$BACKUPFILE"
```

The helper refuses a nonempty destination and restores the archive in one transaction. After it succeeds, compare the source and destination while OpenFGA remains stopped.

```shell
PRIMARY=$(kubectl get cluster.postgresql.cnpg.io platform-mesh-pg \
  -n platform-mesh-system -o jsonpath='{.status.currentPrimary}')
kubectl exec -n platform-mesh-system "$PRIMARY" -c postgres -- \
  psql -X -U postgres -d openfga -v ON_ERROR_STOP=1 \
  -c 'SELECT count(*) FROM store; SELECT count(*) FROM authorization_model; SELECT count(*) FROM tuple;'

set +x
SOURCE_PGPASSWORD=$(kubectl get secret openfga-postgres -n platform-mesh-system \
  -o jsonpath='{.data.postgres-password}' | base64 -d)
printf '%s\0' "$SOURCE_PGPASSWORD" | \
  kubectl exec -i -n platform-mesh-system openfga-postgres-0 -- bash -c '
    IFS= read -r -d "" PGPASSWORD
    export PGPASSWORD
    exec psql -X -U postgres -d postgres -v ON_ERROR_STOP=1 \
      -c "SELECT count(*) FROM store; SELECT count(*) FROM authorization_model; SELECT count(*) FROM tuple;"
  '
unset SOURCE_PGPASSWORD
```

Also compare the recorded store and model IDs. Investigate any difference before continuing.

## 5. Switch to CNPG and resume OpenFGA

Copy the maintenance profile and merge these values into `components.services.openfga.values`. Keep `replicaCount: 0`, preserve the existing migration and image settings, and merge rather than replace existing volume lists.

```shell
cp "$BACKUPDIR/profile-maintenance.yaml" "$BACKUPDIR/profile-cnpg-stopped.yaml"
```

```yaml
postgresql:
  enabled: true
datastore:
  existingSecret: cnpg-openfga-user
  secretKeys:
    passwordKey: password
    usernameKey: username
  uri: "postgres://platform-mesh-pg-rw.platform-mesh-system.svc.cluster.local:5432/openfga?sslmode=verify-full&sslrootcert=/etc/openfga/cnpg-ca/ca.crt"
extraVolumes:
  - name: cnpg-ca
    secret:
      secretName: platform-mesh-pg-ca
extraVolumeMounts:
  - mountPath: /etc/openfga/cnpg-ca
    name: cnpg-ca
    readOnly: true
migrate:
  extraVolumeMounts:
    - mountPath: /etc/openfga/cnpg-ca
      name: cnpg-ca
      readOnly: true
```

Remove an old `datastore.uriSecret` or `secretKeys.uriKey` override if present. Apply the connection change while OpenFGA remains stopped.

```shell
kubectl apply -f "$BACKUPDIR/profile-cnpg-stopped.yaml"
# reconcile
kubectl wait helmrelease/openfga -n platform-mesh-system \
  --for=jsonpath='{.spec.values.datastore.existingSecret}'=cnpg-openfga-user --timeout=300s
kubectl wait deployment/openfga -n platform-mesh-system \
  --for=jsonpath='{.spec.template.spec.volumes[?(@.name=="cnpg-ca")].secret.secretName}'=platform-mesh-pg-ca --timeout=300s
kubectl wait deployment/openfga -n platform-mesh-system \
  --for=jsonpath='{.spec.replicas}'=0 --timeout=300s
```

Confirm the HelmRelease contains the CNPG URI and both the server and migration init container mount the CA. Then resume with autoscaling disabled for the first rollout.

```shell
cp "$BACKUPDIR/profile-cnpg-stopped.yaml" "$BACKUPDIR/profile-cnpg-running.yaml"
# Set replicaCount to ORIGINAL_REPLICAS in data.profile.yaml.
kubectl apply -f "$BACKUPDIR/profile-cnpg-running.yaml"
reconcile
kubectl wait helmrelease/openfga -n platform-mesh-system \
  --for=jsonpath='{.spec.values.replicaCount}'="$ORIGINAL_REPLICAS" --timeout=300s
kubectl wait deployment/openfga -n platform-mesh-system \
  --for=jsonpath='{.spec.replicas}'="$ORIGINAL_REPLICAS" --timeout=300s
kubectl rollout status deployment/openfga -n platform-mesh-system --timeout=300s
```

Verify the recorded stores, models, tuples, and allow/deny checks before reopening traffic. If autoscaling was originally enabled, restore its original settings through the profile and reconcile again.

## 6. Remove the bundled PostgreSQL workload

Check that the source PVC will be retained.

```shell
kubectl get statefulset openfga-postgres -n platform-mesh-system \
  -o jsonpath='{.spec.persistentVolumeClaimRetentionPolicy}{"\n"}'
kubectl get pvc data-openfga-postgres-0 -n platform-mesh-system
```

After application verification, disable the bundled database.

```shell
cp "$BACKUPDIR/profile-cnpg-running.yaml" "$BACKUPDIR/profile-cnpg-final.yaml"
# Set components.services.openfga.values.postgresql.enabled=false.
# Set components.services.openfga.values.replicaCount=1
kubectl apply -f "$BACKUPDIR/profile-cnpg-final.yaml"
# reconcile
kubectl wait helmrelease/openfga -n platform-mesh-system \
  --for=jsonpath='{.spec.values.postgresql.enabled}'=false --timeout=300s
kubectl wait statefulset/openfga-postgres -n platform-mesh-system \
  --for=delete --timeout=300s
kubectl wait pod/openfga-postgres-0 -n platform-mesh-system \
  --for=delete --timeout=300s
kubectl get pvc data-openfga-postgres-0 -n platform-mesh-system
```

Verify OpenFGA reads and writes again. Retain the source PVC and migration archive according to your recovery policy, and include OpenFGA in the CNPG backup policy.

## Recovery

If backup or restore fails, keep OpenFGA stopped. Before cutover, restore the original connection and replica settings. After cutover, switch back only if CNPG has accepted no new writes; otherwise the source is stale and requires a reverse migration. Apply recovery changes through the profile and call `reconcile`.

