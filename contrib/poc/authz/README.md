# Authorization modularization PoC

The main idea of a PoC is to make account access intent explicit and projects it through a generic `AuthorizationGrant` CRD and
independent authorization process into OpenFGA or native kcp RBAC. The PoC tests with a few basic use cases whether account lifecycle management can express who should have access to what without knowing the authorization engine.

For a plain-language detailed explanation of the code and PoC, see [WALKTHROUGH.md](WALKTHROUGH.md).

## What is implemented

- Experimental `core.platform-mesh.io/v1alpha1` AuthorizationGrant, with immutable
  account identity, canonical User subject, role and propagation; mutable
  Active/Revoked state; resolved workspace and reconciliation status.
- Opt-in account initialization/termination through intent. Creator grants are
  created once, retained when revoked, and cleaned up before workspace termination.
- Independent `security-operator authz` process with RBAC and OpenFGA projections.
  In the FGA profile the projection controller and authorization webhook run as
  separate deployments, permitting projection interruption experiments.
- UID-based projection ownership, overlapping grants, retry after an unknown FGA
  write outcome, stale Account UID rejection, and cleanup finalizers.
- Exact-path APIExportPolicy projection. Binding eligibility controls future API
  bindings; resource permissions have a separate lifecycle.
- Explicit headless Tilt profiles and a Go runner that compares actual API access
  with kcp access reviews and writes JSON, Markdown and JUnit evidence.

The existing default composition uses its existing permission writers. The
PoC profile must be selected explicitly.

## Contract used for the PoC

```yaml
apiVersion: core.platform-mesh.io/v1alpha1
kind: AuthorizationGrant
metadata:
  name: example-reader
spec:
  accountRef:
    name: org-a
    uid: REPLACE_WITH_ACCOUNT_UID
  subject:
    kind: User
    name: reader
  roleRef: account-reader
  propagation: Local
  state: Active
```

The resource lives alongside the Account in its parent workspace. Its scope is
**the generated account workspace's contents**. Permissions on the Account object
in its parent workspace are governed independently by that parent's permissions.

| Role           | Permissions                                                                                |
| -------------- | ------------------------------------------------------------------------------------------ |
| account-reader | Workspace entry; get/list/watch AccountInfo, immediate child Account objects, and HTTPBins |
| account-editor | Reader permissions plus create/update/patch/delete HTTPBins                                |

Both roles can create self access reviews. Neither permits Account mutation,
grant management, RBAC management or secret access. Administrators provision the
accounts and namespaces.

`Local` propagation config means that the role is not propagated to descendant workspaces. `Descendants` is implemented
by the FGA model and reported as UnsupportedCapability in RBAC. Grants are additive:
revoking one grant does not remove access independently supplied by another.

Status `Reconciled=True` means backend projection matches the current desired
state.

## Run

Run from the monorepo root to deploy RBAC-Setup:

```sh
task authz-poc:up PROFILE=authz-rbac
```

The up task uses the dedicated kind context `kind-platform-mesh-authz-rbac`.
Repeat the procedure with `authz-openfga` and `authz-baseline` in separate fresh
clusters. The shared Tilt credential export is refreshed by the active deployment;
the evidence collector verifies that it matches the requested profile's cluster.
Run one profile at a time because Tilt's credential export and local build paths
are shared. To repeat from an entirely fresh cluster, remove only the dedicated
PoC cluster you intend to recreate; the up task does not delete existing clusters.

Once infrastructure and the HTTPBin provider are ready, use another terminal to start RBAC demo:

```sh
task authz-poc:demo PROFILE=authz-rbac
```

`demo` first waits for the provider export to publish HTTPBin, then provisions
two organizations, sibling/nested accounts and fixture namespaces,
explicitly binds the HTTPBin API into the organization workspaces and waits for
those APIBindings to become Bound, then resets only runner-owned grants and
executes the scenario file.

The default `cases-rbac.json` and `cases-openfga.json` exercise local access,
HTTPBin write boundaries, denied grant management, organization/descendant
isolation, overlapping grants, revocation and explicit inheritance. The baseline
records legacy creator and unassigned-user behavior without writing grants.

The harness submits requests using synthetic users with `system:authenticated`;
it never forwards `system:masters`. It prefers SelfSubjectAccessReview. If a
revoked identity cannot create that review, it records the distinction and submits
an exact-subject SubjectAccessReview through the same target workspace as the
administrator.

To start OpenFGA setup and demo run:

```sh
docker start platform-mesh-authz-openfga-control-plane
task authz-poc:up PROFILE=authz-openfga
task authz-poc:demo PROFILE=authz-openfga EVIDENCE=contrib/poc/authz/results/fga-run-1
task authz-poc:report EVIDENCE=contrib/poc/authz/results/fga-run-1
```

Each run writes JSON, Markdown and JUnit reports containing scenario outcomes, grant conditions and duration.

## Failure and lifecycle experiments

After fixtures exist, these commands operate only on the dedicated profile's
cluster and preserve their evidence under `results/<profile>-<experiment>`:

```sh
task authz-poc:experiment PROFILE=authz-openfga EXPERIMENT=outage
task authz-poc:experiment PROFILE=authz-openfga EXPERIMENT=restart
task authz-poc:experiment PROFILE=authz-openfga EXPERIMENT=rebuild
task authz-poc:experiment PROFILE=authz-rbac EXPERIMENT=restart
task authz-poc:experiment PROFILE=authz-rbac EXPERIMENT=recreate
```

- `outage`: stop FGA, verify denial and independent administrator access, restore
  FGA and verify recovery. A trap restores the deployment on failure.
- `restart`: stop projection while checks remain available, revoke a grant,
  demonstrate pending projection, restart and verify effective revocation.
- `rebuild`: replay persisted intent into a new empty PoC FGA store and repoint
  the check process; restore original process arguments afterwards. This is an
  engine reconstruction experiment, not a backend-switch migration.
- `recreate`: provision a disposable account, grant access, delete it, recreate
  the same name with a new UID and verify the former user's access does not return.

## Local verification and generation

```sh
task authz-poc:test
go test ./operators/security-operator/internal/subroutine ./operators/security-operator/pkg/config ./operators/security-operator/cmd
go test ./operators/platform-mesh-operator/pkg/subroutines ./operators/platform-mesh-operator/internal/config
task build-security-operator
```

Grant CRDs/deepcopy are generated from the shared API types. Use the repository's
`generate-apis` and `generate-security-operator` tasks when changing the contract.
