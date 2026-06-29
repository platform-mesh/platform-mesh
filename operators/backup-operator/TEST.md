# backup-operator — Test Suite Reference

All test functions across all suites, grouped by file. For each test: what behavior it exercises, what state it sets up, and what it asserts.

---

## Test Tiers

| Tier | Build tag | Command | Cluster? |
|---|---|---|---|
| Unit | _(none)_ | `make test` | No |
| Integration | `integration` | `make test-integration` | No (envtest) |
| Simulated E2E | `e2e` | `make test-e2e-kind` | Yes (kind) |
| Real E2E | `e2e_real` | `make test-e2e-kind-real` | Yes (kind + minio) |

---

## pkg/backup/capture_unit_test.go — Unit

### TestCapture_EtcdDisabled
- **Tests:** Process is a no-op when `Etcd.Enabled=false` in spec
- **Setup:** PlatformBackup with `Components.Etcd.Enabled=false`
- **Asserts:** Result is Continue; `Status.Artefacts.Etcd` remains nil

### TestCapture_AlreadyCaptured
- **Tests:** Idempotency — second call skips all work when artefacts already recorded
- **Setup:** PlatformBackup with pre-populated artefacts, snapshot key `"rev-99"`
- **Asserts:** Result is Continue; key unchanged at `"rev-99"`

### TestCapture_WrongObjectType
- **Tests:** Error when Process receives a non-PlatformBackup object
- **Setup:** PlatformRestore passed to EtcdCaptureSubroutine
- **Asserts:** Error containing `"unexpected object type"`

### TestCapture_NoShards
- **Tests:** Operator stops (does not error) when no kcp-shard Etcd CRs exist
- **Setup:** Empty cluster (no Etcd CRs)
- **Asserts:** Result is StopWithRequeue; no error

### TestCapture_SingleShard_Success
- **Tests:** Happy path — task succeeds and lease key is recorded
- **Setup:** One shard, task interceptor returns `TaskStateSucceeded`, lease returns `"rev-100"`
- **Asserts:** Result is Continue; `Shards["shard-a"].SnapshotKey = "rev-100"`; `SnapshotTime` non-zero

### TestCapture_MultiShard_Success
- **Tests:** All shards captured and all snapshot keys recorded correctly
- **Setup:** Two shards, tasks succeed, leases return `"rev-1"` and `"rev-2"`
- **Asserts:** Result is Continue; artefacts contain 2 shards; keys match per-shard

### TestCapture_TaskFailed_PropagatesError
- **Tests:** Failed EtcdOpsTask surfaces its last error description
- **Setup:** Task with `TaskStateFailed`, `LastError = "disk full"`
- **Asserts:** Error returned containing `"disk full"`

### TestCapture_TaskRejected_PropagatesError
- **Tests:** Rejected EtcdOpsTask surfaces as error
- **Setup:** Task with `TaskStateRejected`, `LastError = "backup is not enabled for etcd"`
- **Asserts:** Error returned containing that message

### TestCapture_TaskFailed_NoLastError
- **Tests:** Fallback to state name when `LastErrors` is empty
- **Setup:** Task `TaskStateFailed`, no `LastErrors`
- **Asserts:** Error returned containing state name `"TaskStateFailed"`

### TestCapture_LeaseNotUpdated
- **Tests:** Unchanged lease is accepted when task succeeds (on-demand snapshots don't bump `HolderIdentity`)
- **Setup:** Lease `HolderIdentity = "rev-old"` before and after, task `Succeeded`
- **Asserts:** No error; Process succeeds

### TestCapture_EmptyLeaseOnFreshCluster
- **Tests:** Fresh cluster with empty lease accepts `TaskSucceeded` as authoritative signal
- **Setup:** Lease with nil/empty `HolderIdentity`, task `Succeeded`
- **Asserts:** No error; Process succeeds

### TestCapture_LeaseKeyMatchesBaseline
- **Tests:** Unchanged lease (baseline == current) accepted when task succeeded
- **Setup:** Lease `"rev-old"` at both baseline and current, task `Succeeded`
- **Asserts:** No error; `SnapshotKey = "rev-old"`

### TestCapture_PollTimeout
- **Tests:** Context timeout surfaces as deadline-exceeded error
- **Setup:** Task with no state; context timeout 50ms
- **Asserts:** Error containing `"context deadline exceeded"`

### TestCapture_GetName
- **Tests:** Subroutine returns the expected condition name constant
- **Setup:** EtcdCaptureSubroutine created
- **Asserts:** `GetName() == backup.ConditionEtcdSnapshotted`

### TestCapture_TaskDeletedAfterSuccess
- **Tests:** EtcdOpsTask is deleted after `Succeeded` so the namespace stays clean
- **Setup:** Task with `Succeeded`
- **Asserts:** After Process, `Get(taskName)` returns NotFound

### TestCapture_TaskDeletedAfterFailure
- **Tests:** EtcdOpsTask is deleted after failure so retries can create a fresh task
- **Setup:** Task with `Failed`
- **Asserts:** Error returned; after Process, `Get(taskName)` returns NotFound

### TestCapture_TaskNotFound_LeaseUpdated
- **Tests:** Task gone but lease already updated → treat as complete (prior reconcile succeeded)
- **Setup:** Task not found on second poll; lease baseline=`""`, current=`"rev-already"`
- **Asserts:** No error; `SnapshotKey = "rev-already"`

### TestCapture_TaskNotFound_LeaseNotUpdated
- **Tests:** Task gone and lease unchanged → error so reconcile retries
- **Setup:** Task and lease both NotFound
- **Asserts:** Error containing `"not found and full-snap lease is unchanged"`

### TestOpsTaskName_ShortNames
- **Tests:** Short names produce valid k8s names with hash suffix
- **Setup:** `OpsTaskName("my-backup", "my-shard")`
- **Asserts:** Name ≤ 253 chars; non-empty; different inputs → different names

### TestOpsTaskName_LongNamesTruncated
- **Tests:** Names exceeding 253 chars are truncated while preserving hash suffix
- **Setup:** 200-char backup + 200-char etcd names
- **Asserts:** Result ≤ 253 chars; different long inputs → different names; 6-char hex suffix present

### TestOpsTaskName_Deterministic
- **Tests:** Same inputs always produce the same name
- **Setup:** OpsTaskName called twice with identical args
- **Asserts:** Both calls return identical strings

### TestOpsTaskName_AmbiguousInputsSeparated
- **Tests:** Hash disambiguates when concatenated base strings match
- **Setup:** `OpsTaskName("a-b", "c")` vs `OpsTaskName("a", "b-c")`
- **Asserts:** Two calls produce different names

---

## pkg/backup/capture_test.go — Integration

### TestCapture_MultiShard_Success
- **Tests:** Multi-shard backup with real k8s API and task simulator
- **Setup:** Two Etcd shards; `runTaskSimulator` marks tasks `Succeeded`
- **Asserts:** Result is Continue; artefacts contain 2 shards with non-empty keys and non-zero times

### TestCapture_TaskFailed_ReturnsError
- **Tests:** Failed task causes error containing failure description
- **Setup:** Task simulator marks tasks `Failed` with `"simulated backup-restore failure"`
- **Asserts:** Error returned containing that message

### TestCapture_Idempotent
- **Tests:** Second Process call on same backup is a no-op
- **Setup:** PlatformBackup with pre-populated `Status.Artefacts.Etcd`
- **Asserts:** Result is Continue; key unchanged at `"already-captured"`

---

## pkg/restore/restore_unit_test.go — Unit

### TestRestore_WrongObjectType
- **Tests:** Error when Process receives non-PlatformRestore object
- **Setup:** PlatformBackup passed to EtcdRestoreSubroutine
- **Asserts:** Error containing `"unexpected object type"`

### TestRestore_BackupNotFound
- **Tests:** StopWithRequeue when source backup is missing
- **Setup:** PlatformRestore referencing nonexistent backup
- **Asserts:** Result is StopWithRequeue; no error

### TestRestore_NoEtcdArtefacts
- **Tests:** Backup with empty etcd artefacts is skipped gracefully
- **Setup:** PlatformBackup with nil `Status.Artefacts.Etcd`
- **Asserts:** Result is Continue; no error

### TestRestore_NilEtcdArtefacts
- **Tests:** Nil etcd artefact field is skipped without panic
- **Setup:** PlatformBackup with no artefact initialization
- **Asserts:** Result is Continue; no error

### TestRestore_ShardNotFound
- **Tests:** Missing Etcd CR returns error
- **Setup:** Backup has artefact for `"missing-shard"`; no matching Etcd CR in cluster
- **Asserts:** Error containing `"not found"`

### TestRestore_SingleShard_AnnotationSet
- **Tests:** First reconcile triggers delete+recreate and sets restore annotation; returns Pending
- **Setup:** Backup with one shard artefact; shard CR without annotation
- **Asserts:** Result is Pending; recreated shard has `AnnotationKeyRestoredFromSnapshot = "rev-42"`

### TestRestore_SingleShard_ReadyAfterAnnotation
- **Tests:** OK returned when annotation set and `status.ready=true`
- **Setup:** Shard with restore annotation and `Status.Ready=true`
- **Asserts:** Result is Continue; no error

### TestRestore_SingleShard_PendingWhenNotReady
- **Tests:** Pending returned when annotation set but `status.ready=false`
- **Setup:** Shard with restore annotation but `Status.Ready=false`
- **Asserts:** Result is Pending

### TestRestore_SingleShard_SpecPreserved
- **Tests:** Recreated Etcd CR has original spec fields (e.g., Replicas)
- **Setup:** Original shard with `Spec.Replicas=3`
- **Asserts:** After delete+recreate, `recreated.Spec.Replicas == 3`

### TestRestore_MultiShard_AllRestored
- **Tests:** All shards with annotation and `ready=true` → Process returns OK
- **Setup:** Two shards both ready with correct annotations
- **Asserts:** Result is Continue; no error

### TestRestore_MultiShard_OnePending
- **Tests:** Pending when one shard is ready but the other is not
- **Setup:** shard-a ready; shard-b has annotation but not ready
- **Asserts:** Result is Pending

### TestRestore_MultiShard_FirstFailureHalts
- **Tests:** First shard error is returned and identifies the failing shard
- **Setup:** shard-a present and ready; shard-b absent
- **Asserts:** Error returned containing `"shard-b"`

### TestRestore_GetName
- **Tests:** Subroutine returns expected condition name
- **Setup:** EtcdRestoreSubroutine created
- **Asserts:** `GetName() == restore.ConditionEtcdRestored`

### TestRestore_Idempotency
- **Tests:** Re-reconcile when `EtcdRestored=True` already set is a no-op
- **Setup:** PlatformRestore with `EtcdRestored=True` condition; no Etcd CRs in cluster
- **Asserts:** Result is Continue; no error (guard prevents touching cluster)

### TestRestore_EmptySnapshotKey
- **Tests:** Empty `SnapshotKey` in artefact is rejected up-front before touching any CR
- **Setup:** Backup artefact with `SnapshotKey=""`
- **Asserts:** Error containing `"empty snapshot key"`

### TestRestore_CreateAndWait_AlreadyExists
- **Tests:** Race where etcd-druid recreates CR between operator delete and Create is handled gracefully
- **Setup:** Create interceptor returns AlreadyExists; re-creates CR with no annotation/label
- **Asserts:** Result is Pending (not error); `patchCalled=true`; annotation and kcp-shard label patched onto raced CR

---

## pkg/restore/restore_test.go — Integration

### TestRestore_SingleShard_Recreate
- **Tests:** Single shard delete+recreate with real k8s API and envtest
- **Setup:** Shard + backup with one artefact; restore references backup
- **Asserts:** Eventually Process returns Continue; recreated shard has annotation `"rev-42"`

### TestRestore_MultiShard_ConcurrentRecreate
- **Tests:** Two shards recreated and annotations set correctly
- **Setup:** Two shards with artefacts
- **Asserts:** Eventually Continue; both recreated shards have non-empty restore annotations

### TestRestore_MissingBackup_StopsWithRequeue
- **Tests:** Missing backup produces StopWithRequeue
- **Setup:** PlatformRestore referencing nonexistent backup
- **Asserts:** Result is StopWithRequeue

### TestRestore_MissingEtcdArtefacts_Skips
- **Tests:** Backup with no etcd artefacts skips restore work
- **Setup:** PlatformBackup with no `Status.Artefacts.Etcd`
- **Asserts:** Result is Continue

---

## pkg/topology/validate_test.go — Unit

### TestTopologyValidate_NonStrict
- **Tests:** Non-strict mode skips topology validation entirely
- **Setup:** PlatformRestore with `TopologyValidation=""`
- **Asserts:** Result is Skip (not Continue)

### TestTopologyValidate_Idempotent
- **Tests:** Already-validated restore is idempotent
- **Setup:** PlatformRestore with `TopologyValidated=True` condition
- **Asserts:** Result is Continue

### TestTopologyValidate_SourceBackupNotFound
- **Tests:** Missing backup produces StopWithRequeue (not hard error)
- **Setup:** PlatformRestore referencing nonexistent backup
- **Asserts:** Result is StopWithRequeue; no error

### TestTopologyValidate_NoEtcdArtefacts
- **Tests:** Backup with no etcd artefacts skips validation
- **Setup:** PlatformBackup with nil artefacts
- **Asserts:** Result is Skip

### TestTopologyValidate_ShardSetsMatch
- **Tests:** Matching shard sets pass validation
- **Setup:** Backup has `{shard-a, shard-b}`; live cluster has both
- **Asserts:** Result is Continue; no error

### TestTopologyValidate_ShardMissingFromCluster
- **Tests:** Shard in backup but absent from live cluster → StopWithRequeue
- **Setup:** Backup has `{shard-a, shard-b}`; live has only `shard-a`
- **Asserts:** Result is StopWithRequeue; message contains `"shard-b"` and `"missing from live cluster"`

### TestTopologyValidate_ExtraShardInCluster
- **Tests:** Extra shard in live cluster not in backup → StopWithRequeue
- **Setup:** Backup has `{shard-a}`; live has `{shard-a, shard-extra}`
- **Asserts:** Result is StopWithRequeue; message contains `"shard-extra"` and `"absent from backup"`

### TestTopologyValidate_MultipleErrors
- **Tests:** Multiple mismatches reported in a single message
- **Setup:** Backup has `{shard-a, shard-missing}`; live has `{shard-a, shard-extra}`
- **Asserts:** Result is StopWithRequeue; message contains both `"shard-missing"` and `"shard-extra"`

### TestTopologyValidate_WrongObjectType
- **Tests:** Error when Process receives non-PlatformRestore
- **Setup:** PlatformBackup passed to ValidateSubroutine
- **Asserts:** Error containing `"unexpected object type"`

---

## pkg/topology/topology_test.go — Unit

### TestMarshalUnmarshalRoundTrip
- **Tests:** Marshal → Unmarshal produces identical Manifest struct
- **Setup:** Sample manifest with KCP, CNPG, OpenFGA topology
- **Asserts:** All fields equal after round-trip; `SchemaVersion`, timestamps, host cluster, shards, clusters, stores

### TestUnmarshalMissingRequiredField
- **Tests:** Unmarshal rejects document missing required field
- **Setup:** JSON missing `"schemaVersion"` key
- **Asserts:** Error returned; error is `*topology.ValidationError` with non-empty `SchemaErrors`

### TestUnmarshalBadDigest
- **Tests:** Unmarshal rejects malformed SHA-256 digest
- **Setup:** JSON with `"not-a-sha256"` as `logicalClusterIDsDigest`
- **Asserts:** Error returned; error is `*topology.ValidationError`

### TestValidateIdentical
- **Tests:** Validate returns nil when source and target are identical
- **Setup:** Same manifest for both source and target
- **Asserts:** No error

### TestValidateShardDigestMismatch
- **Tests:** Validate detects changed shard digest
- **Setup:** Source and target with one shard's `logicalClusterIDsDigest` changed
- **Asserts:** Error is `*topology.MismatchError`; `Fields` contains `"kcp.shards[root].logicalClusterIDsDigest"`

### TestValidateExtraShardOnTarget
- **Tests:** Extra shard on target detected as mismatch
- **Setup:** Source has 2 shards; target has 3
- **Asserts:** Error is `*topology.MismatchError`; `Fields` contains `"kcp.shards[shard-b]"`

### TestDigestStable
- **Tests:** `Digest()` returns stable hex string across calls
- **Setup:** Sample manifest
- **Asserts:** Two calls return identical non-empty strings with `"sha256:"` prefix

### TestRFC009SampleDocument
- **Tests:** RFC 009 sample document parses and self-validates
- **Setup:** Sample JSON from RFC 009 spec
- **Asserts:** Unmarshal succeeds; `Validate(m, m)` returns no error

### TestValidateExtraCNPGClusterOnTarget
- **Tests:** Extra CNPG cluster on target detected
- **Setup:** Source has 1 CNPG cluster; target has 2
- **Asserts:** Error `*topology.MismatchError`; `Fields` contains `"cnpg.clusters[db-extra]"` with `source="<missing>"`

### TestValidateExtraOpenFGAStoreOnTarget
- **Tests:** Extra OpenFGA store on target detected
- **Setup:** Source has 1 store; target has 2
- **Asserts:** Error `*topology.MismatchError`; `Fields` contains `"openfga.stores[store-extra]"`

### TestValidateCNPGMajorVersionMismatch
- **Tests:** CNPG major version change detected (e.g. Postgres upgrade)
- **Setup:** Source has `MajorVersion=15`; target has `16`
- **Asserts:** Error `*topology.MismatchError`; `Fields` contains `"cnpg.clusters[db-a].majorVersion"` with `source="15"`, `target="16"`

### TestValidateDuplicateShardNames
- **Tests:** Duplicate shard names in source produce mismatch sentinel
- **Setup:** Source has two shards both named `"shard-a"`; target has one
- **Asserts:** Error returned (duplicate names cause mismatch)

---

## pkg/e2e/etcddruid_test.go — Simulated E2E

### TestEtcDruid_CaptureRoundTrip
- **Tests:** Full backup→restore round-trip with simulated etcd-druid
- **Setup:** Real Etcd CR labeled kcp-shard; task simulator; ready simulator; PlatformBackup + PlatformRestore
- **Asserts:**
  - `EtcdSnapshotted=True`; snapshot key non-empty; `SnapshotTime` non-zero
  - `EtcdRestored=True`
  - Recreated Etcd CR has `AnnotationKeyRestoredFromSnapshot` matching key
  - kcp-shard label present on recreated CR

### TestEtcDruid_Restore_MultiShard
- **Tests:** Multi-shard backup and restore with per-shard annotations
- **Setup:** Two Etcd shards; 2-shard backup; 2-shard restore
- **Asserts:**
  - `EtcdSnapshotted=True` with 2 shards in artefacts
  - `TopologyValidated=True`; `EtcdRestored=True`
  - Each recreated shard has correct per-shard snapshot annotation

### TestEtcDruid_Capture_MultiShard
- **Tests:** Fan-out capture of multiple shards in parallel
- **Setup:** Two Etcd shards; one PlatformBackup
- **Asserts:** `EtcdSnapshotted=True`; both shards have non-empty snapshot keys

### TestEtcDruid_Capture_Idempotent
- **Tests:** Second backup run produces same key (no new EtcdOpsTask)
- **Setup:** First backup completed; force second reconcile via annotation patch
- **Asserts:** First and second snapshot keys identical

### TestEtcDruid_Restore_TopologyAware
- **Tests:** Condition sequence: `EtcdSnapshotted=True` → `TopologyValidated=True` → `EtcdRestored=True`
- **Setup:** One shard backup; restore with `TopologyValidation=Strict`
- **Asserts:** Both `TopologyValidated=True` and `EtcdRestored=True` present on final restore

---

## pkg/e2e/etcddruid_failure_test.go — Simulated E2E

### TestEtcDruid_Capture_NoShards
- **Tests:** Backup with no kcp-shard Etcd CRs sets `EtcdSnapshotted=False/Stopped`
- **Setup:** PlatformBackup; no Etcd CRs in namespace
- **Asserts:** `EtcdSnapshotted=False`; `Reason="Stopped"` (requeue not error); message explains no shards found

### TestEtcDruid_Capture_TaskFailed
- **Tests:** Failed EtcdOpsTask surfaces `EtcdSnapshotted=False`; task deleted for retry
- **Setup:** Task injected with `Failed` state + `"simulated etcd snapshot error"`
- **Asserts:** `EtcdSnapshotted=False`; EtcdOpsTask deleted after failure

### TestEtcDruid_Capture_TaskRejected
- **Tests:** Rejected EtcdOpsTask causes `EtcdSnapshotted=False`; task deleted
- **Setup:** Task injected with `Rejected` state
- **Asserts:** `EtcdSnapshotted=False`; EtcdOpsTask deleted

### TestEtcDruid_Capture_LeaseNotUpdated
- **Tests:** Task Succeeded but lease unchanged → `EtcdSnapshotted=False`
- **Setup:** Task marked `Succeeded`; full-snap lease not updated
- **Asserts:** `EtcdSnapshotted=False`

### TestEtcDruid_Restore_MissingBackup
- **Tests:** Restore requeues gracefully when backup not found
- **Setup:** PlatformRestore referencing nonexistent backup
- **Asserts:** Condition set within timeout; `EtcdRestored` not True

### TestEtcDruid_Restore_MissingEtcdShard
- **Tests:** Topology gate blocks restore when backup shard absent from cluster
- **Setup:** Backup has `"ghost-shard"` artefact; no Etcd CR with that name exists
- **Asserts:** `TopologyValidated=False/Stopped`; message mentions `"ghost-shard"`; `EtcdRestored=Unknown` (chain not reached)

### TestEtcDruid_Restore_EtcdNotReady
- **Tests:** Operator waits patiently when restored Etcd takes time to reach `ready=true`
- **Setup:** `slowReadySimulator` delays `status.ready=true` by 20s
- **Asserts:** `EtcdRestored=True` eventually (within 6m); correct annotation on recreated CR

### TestEtcDruid_Restore_TopologyMismatch_ExtraLiveShard
- **Tests:** Extra shard in live cluster → `TopologyValidated=False`; restore blocked
- **Setup:** Backup has `shard-a`; live has `{shard-a, shard-extra}`
- **Asserts:** `TopologyValidated=False/Stopped`; message contains `"shard-extra"`; `EtcdRestored=Unknown`

### TestEtcDruid_Restore_TopologyMatch_Passes
- **Tests:** Restore succeeds when live shard set matches backup exactly
- **Setup:** Backup has `shard-a`; live has `shard-a`; restore with Strict
- **Asserts:** `TopologyValidated=True`; `EtcdRestored=True`

### TestEtcDruid_Restore_TopologyMismatch_ShardMissingFromCluster
- **Tests:** Topology gate blocks when backup shard missing from live cluster
- **Setup:** Backup has `{shard-present, shard-gone}`; live has only `shard-present`
- **Asserts:** `TopologyValidated=False/Stopped`; message contains `"shard-gone"` and `"missing from live cluster"`; `EtcdRestored=Unknown`

### TestEtcDruid_Restore_TopologyMismatch_BothDirections
- **Tests:** Both mismatch directions reported together in condition message
- **Setup:** Backup has `shard-recorded`; live has `shard-live`
- **Asserts:** `TopologyValidated=False`; message mentions both `"shard-recorded"` and `"shard-live"`

### TestEtcDruid_Restore_TopologyMismatch_SelfHealing
- **Tests:** Restore recovers automatically once extra shard removed from cluster
- **Setup:** 2 live shards (1 in backup, 1 extra); extra shard deleted mid-test
- **Asserts:**
  - `TopologyValidated=False` while extra shard present
  - After deletion + requeue: `TopologyValidated=True`; `EtcdRestored=True`

### TestEtcDruid_Restore_TopologyNonStrict_IgnoresMismatch
- **Tests:** Non-strict mode bypasses topology check and completes restore despite mismatch
- **Setup:** `TopologyValidation=None`; backup has 1 shard; live has 2
- **Asserts:** `EtcdRestored=True`; `TopologyValidated` absent or `True/Skipped`

---

## pkg/e2e/real/backup_test.go — Real E2E

### TestRealEtcd_Backup_SingleShard
- **Tests:** Single shard backup against real etcd pods and minio
- **Setup:** Real Etcd CR; wait for `Ready=true`; create PlatformBackup
- **Asserts:**
  - `EtcdSnapshotted=True`; snapshot key non-empty; `SnapshotTime` non-zero
  - S3 snapshot object exists in minio under shard prefix

### TestRealEtcd_Backup_Idempotent
- **Tests:** Second backup of same shard records same key (no new task triggered)
- **Setup:** Two sequential PlatformBackups for same shard
- **Asserts:** Both backups complete; snapshot keys identical

### TestRealEtcd_Backup_ContentIntegrity
- **Tests:** Data written before backup is readable after restore
- **Setup:** Write key `/e2e/integrity-check=platform-mesh-e2e-real` → backup → restore → read
- **Asserts:**
  - Backup completes (`EtcdSnapshotted=True`)
  - Restore completes (`EtcdRestored=True`); restored shard `Ready=true`
  - Pre-backup key/value present after restore

### TestRealEtcd_Backup_NoShards
- **Tests:** Backup with no kcp-shard Etcd CRs sets `EtcdSnapshotted=False/Stopped`
- **Setup:** PlatformBackup; no Etcd CRs in namespace
- **Asserts:** `EtcdSnapshotted=False`; `Reason="Stopped"`; message non-empty

### TestRealEtcd_Backup_ShardDeletedDuringBackup
- **Tests:** Backup handles gracefully if shard deleted while task in-flight
- **Setup:** Create shard → create backup → wait for task → delete shard
- **Asserts:** Backup eventually settles; no panic; condition set with Error or Stopped reason

### TestRealEtcd_Backup_AfterRestore
- **Tests:** Restored cluster can be backed up again
- **Setup:** Backup → restore (shard ready) → second backup
- **Asserts:**
  - First backup completes with snapshot key
  - Restore completes; restored shard `Ready=true`
  - Second backup completes with non-empty key; S3 snapshot exists

---

## pkg/e2e/real/restore_test.go — Real E2E

### TestRealEtcd_Restore_SingleShard
- **Tests:** Full backup→restore round-trip with real etcd cluster
- **Setup:** Real Etcd CR; backup; restore
- **Asserts:**
  - Restore completes (`EtcdRestored=True`)
  - Recreated Etcd CR has `AnnotationKeyRestoredFromSnapshot` matching snapshot key
  - kcp-shard label present
  - Restored Etcd cluster reaches `Ready=true`

### TestRealEtcd_Restore_SlowReady
- **Tests:** `EtcdRestored=True` set only after `Etcd.Status.Ready=true` (operator waits)
- **Setup:** Backup; restore; time the readiness wait
- **Asserts:** `EtcdRestored=True` confirmed; restored `Ready=true`; operator waited for readiness

### TestRealEtcd_Restore_SourceBackupNotFound
- **Tests:** Restore with missing backup requeues with Stopped reason
- **Setup:** PlatformRestore referencing nonexistent backup
- **Asserts:** `TopologyValidated=False/Stopped`; `EtcdRestored` not True

### TestRealEtcd_Restore_BackupWithNoEtcdArtefacts
- **Tests:** Restore of backup with no etcd artefacts skips etcd restore work
- **Setup:** PlatformBackup with etcd artefacts manually cleared; PlatformRestore created
- **Asserts:** Restore completes (`Ready=True`); no Etcd CR deleted/recreated

### TestRealEtcd_Restore_Idempotent
- **Tests:** Re-reconcile after `EtcdRestored=True` does not re-delete+recreate the shard
- **Setup:** Restore completes; record Etcd CR UID; force re-reconcile; wait 15s
- **Asserts:** `EtcdRestored` remains True; Etcd CR UID unchanged

### TestRealEtcd_Restore_MissingEtcdShard
- **Tests:** Topology gate blocks restore when backup shard is absent from live cluster
- **Setup:** Backup shard; delete shard CR; create restore
- **Asserts:** `TopologyValidated=False/Stopped`; message mentions missing shard; `EtcdRestored=Unknown`

### TestRealEtcd_Restore_ConcurrentSameBackup
- **Tests:** Two restores referencing the same backup handle the race cleanly
- **Setup:** Two PlatformRestores created concurrently for the same backup
- **Asserts:** Both restores complete with `EtcdRestored` condition set; Etcd CR has correct annotation; no corruption

### TestRealEtcd_Restore_TopologyMismatch_ExtraLiveShard
- **Tests:** Extra live shard blocks restore; original shard untouched
- **Setup:** Backup one shard; add extra shard after backup; create restore
- **Asserts:** `TopologyValidated=False/Stopped`; message contains extra shard name; `EtcdRestored=Unknown`; original shard has no restore annotation

### TestRealEtcd_Restore_TopologyMatch_FullRoundTrip
- **Tests:** Full topology-aware restore preserves pre-backup etcd data
- **Setup:** Write key → backup → restore with `TopologyValidation=Strict` → read key
- **Asserts:** `TopologyValidated=True`; `EtcdRestored=True`; restored shard `Ready=true`; pre-backup key/value present

### TestRealEtcd_Restore_CorruptTopologyAfterBackup
- **Tests:** Topology gate protects original shard when cluster topology corrupted post-backup
- **Setup:** Write key → backup → add extra shard → create restore → check key readable
- **Asserts:** `TopologyValidated=False`; original shard data unchanged (key still readable); restore blocked before any delete+recreate

### TestRealEtcd_Restore_CorruptEtcdAfterBackup
- **Tests:** Pre-backup data survives restore after post-backup writes
- **Setup:** Write pre-backup key → backup → restore
- **Asserts:** `TopologyValidated=True`; `EtcdRestored=True`; restored shard `Ready=true`; pre-backup key present

---

## pkg/e2e/real/sharded_test.go — Real E2E

### TestRealEtcd_Sharded_BackupRestore
- **Tests:** N-shard production scenario: all shards backed up and restored in parallel
- **Setup:** Discover live shard count; create N synthetic shards; wait for all `Ready=true`; backup; restore
- **Asserts:**
  - `EtcdSnapshotted=True` with N shards in artefacts
  - Each shard has non-empty snapshot key and non-zero `SnapshotTime`
  - All N snapshots exist in minio
  - `EtcdRestored=True`; each recreated shard has correct per-shard annotation
  - All N restored shards reach `Ready=true`

### TestRealEtcd_Sharded_ContentIntegrity
- **Tests:** Each shard's unique data survives concurrent restore
- **Setup:** N shards; write unique key/value to each (`/e2e/integrity-sharded=shard-{i}-{id}`) → backup → restore
- **Asserts:**
  - All N shards `Ready=true` pre-backup
  - Backup and restore complete
  - All N restored shards `Ready=true`
  - Each shard's unique key/value readable after restore

---

## Summary

| Suite | File | Tests |
|---|---|---|
| Unit | `pkg/backup/capture_unit_test.go` | 22 |
| Integration | `pkg/backup/capture_test.go` | 3 |
| Unit | `pkg/restore/restore_unit_test.go` | 16 |
| Integration | `pkg/restore/restore_test.go` | 4 |
| Unit | `pkg/topology/validate_test.go` | 9 |
| Unit | `pkg/topology/topology_test.go` | 13 |
| Simulated E2E | `pkg/e2e/etcddruid_test.go` | 5 |
| Simulated E2E | `pkg/e2e/etcddruid_failure_test.go` | 13 |
| Real E2E | `pkg/e2e/real/backup_test.go` | 6 |
| Real E2E | `pkg/e2e/real/restore_test.go` | 11 |
| Real E2E | `pkg/e2e/real/sharded_test.go` | 2 |
| **Total** | | **104** |
