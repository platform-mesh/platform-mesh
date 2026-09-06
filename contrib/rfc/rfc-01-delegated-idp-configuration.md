# RFC: Delegated Identity-Provider Configuration

## Status: Draft — Upstream IdP superseded by design doc

## Authors
- (add names)

## Date: 2026-07-23

> **Upstream IdP:** authoritative doc is
> [`docs/design/upstream-identity-provider-registration.md`](../docs/design/upstream-identity-provider-registration.md)
> (`IdPRegistration`, allowlist, direct Keycloak reconcile, no IDPC merge for tenants).
> Older “verbatim / merge into IDPC” wording below is obsolete for upstreams.

## Related work
- [platform-mesh/backlog#286](https://github.com/platform-mesh/backlog/issues/286) — upstream identity provider epic (moved `IdentityProviderConfiguration` to `root:orgs`)
- [platform-mesh/backlog#300](https://github.com/platform-mesh/backlog/issues/300) — OpenBao operator OIDC auth workflow
- [platform-mesh/platform-mesh#148](https://github.com/platform-mesh/platform-mesh/pull/148) — basic upstream identity provider implementation (in review)

---

## 0. Background & terminology

This RFC is self-contained; the concepts below are the minimum needed to follow it.

- **kcp** — a Kubernetes-like control plane that serves many isolated **workspaces**. Each workspace behaves like its own cluster with its own API surface and access control. Workspaces are arranged in a path hierarchy (e.g. `root:orgs`, `root:orgs:acme`, `root:providers:openbao`).
- **Workspace boundaries** — who can read/write a workspace is controlled per workspace. A tenant can write in the workspace they own but not in a parent or sibling workspace. This is the platform's core isolation mechanism.
- **CRD / custom resource (CR)** — a declarative API object (like any Kubernetes resource). An **operator** is a controller that watches CRs and reconciles the real world to match them.
- **Keycloak** — the identity provider (IdP) that actually authenticates users. Platform Mesh runs **one Keycloak realm per organization**.
- **OIDC client** — an application registered in a realm that can start logins / validate tokens (e.g. `portal`, `kubectl`). A *confidential* client also gets a `client_secret`.
- **Upstream identity provider** — an *external* IdP (e.g. corporate Azure AD, Google) that a realm federates with, so users log in with their existing corporate identity.
- **security-operator** — the Platform Mesh operator that owns all identity reconciliation. It is the only component that writes into the protected `root:orgs` workspace.
- **OpenFGA (`bind` relation)** — the fine-grained authorization system. A `bind` tuple encodes a relationship like "org O may consume provider P". The platform already checks these relations elsewhere.
- **`APIExportPolicy`** — an existing CRD that demonstrates the delegation pattern this RFC reuses: an actor declares intent in a workspace it owns, and the security-operator performs the resulting privileged write somewhere the actor cannot reach.

## 1. Current state of Platform Mesh

Platform Mesh uses **kcp** to give every tenant an isolated, Kubernetes-like workspace. Identity is handled by **Keycloak** (one realm per organization), and the desired state of each realm is expressed declaratively through a single CRD, `IdentityProviderConfiguration` (IDPC).

The IDPC already models everything a realm needs:

```130:132:platform-mesh/apis/core/v1alpha1/identityproviderconfiguration_types.go
	RegistrationAllowed       bool                           `json:"registrationAllowed,omitempty"`
	Clients                   []IdentityProviderClientConfig `json:"clients"`
	UpstreamIdentityProviders []UpstreamIdentityProvider     `json:"upstreamIdentityProviders,omitempty"`
```

- `Clients` — OIDC clients that authenticate against the org realm (e.g. `portal`, `kubectl`).
- `UpstreamIdentityProviders` — external IdPs an org federates with (e.g. corporate Azure AD / Google), already provider-agnostic (`EmailDomainRouting`, generic `OIDC` block).

The **security-operator** is the sole reconciler of IDPC. It writes realms, clients and upstream IdPs into Keycloak and stores generated client secrets.

**Critical boundary:** IDPC lives in the **`root:orgs`** workspace, which is intentionally *not* writable by tenants. Per backlog#286: *"`IdentityProviderConfiguration` has intentionally been moved to the `root:orgs` workspace to prevent the user from updating it."* Only the security-operator writes there.

There is a proven pattern for "let an actor express intent in a workspace they own, and have the security-operator perform the privileged cross-boundary write": **`APIExportPolicy`**. A provider declares an `APIExportPolicy` in its own workspace; the security-operator reconciles it into FGA tuples elsewhere.

```30:36:platform-mesh/apis/core/v1alpha1/apiexportpolicy_types.go
type APIExportPolicySpec struct {
	APIExportRef APIExportRef `json:"apiExportRef"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	AllowPathExpressions []string `json:"allowPathExpressions"`
}
```

---

## 2. Problems that need to be solved

The IDPC data model is fine. The problem is **who can contribute to it, and how**, given the `root:orgs` boundary. Two different actors need to add to an org's IDPC but neither can (or should) write to `root:orgs`:

1. **Org owners cannot configure their own upstream IdPs.**
   An org owner wanting "log in with our corporate IdP" must edit `UpstreamIdentityProviders` in IDPC — but that object sits in `root:orgs`, which they cannot access. Today this only works with an admin kubeconfig. This is exactly the gap raised on platform-mesh#148 ("`root:orgs` IDP resource is not accessible to org owners") and the reason the current PR relies on seed config that reviewers pushed back on.

2. **Provider operators cannot register OIDC clients.**
   A provider like OpenBao (backlog#300) needs an OIDC client in the org realm to authenticate its users/machines. Its original proposal reached for Crossplane and implied direct IDP access — both of which cross the `root:orgs` boundary.

Additional constraints that any solution must respect:

- **No new `root:orgs` access for tenants or providers** — the boundary is the security model, not an inconvenience.
- **Provider-agnostic** — the platform must not grow OpenBao-specific (or Keycloak-specific) surface. Raised on #148.
- **Secret delivery** — a confidential client's `client_secret` is generated in `root:orgs`; the provider must receive it without reading `root:orgs`.
- **Authorization** — writing the intent CR must not by itself authorize a client/IdP in an arbitrary org; the operator must verify opt-in.
- **Input validation** — actor-authored fields (redirect URIs, client names) must be validated before they reach Keycloak.

```mermaid
flowchart LR
  subgraph writable [Workspaces actors CAN write]
    orgOwner["Org owner (root:orgs:&lt;org&gt;)"]
    provider["Provider e.g. OpenBao (root:providers:&lt;name&gt;)"]
  end
  subgraph protected [root:orgs - NOT writable by them]
    idpc[IdentityProviderConfiguration]
  end
  orgOwner -.->|"no write access"| idpc
  provider -.->|"no write access"| idpc
</parameter>
```

---

## 3. Proposal (Option 1: two purpose-built intent CRDs)

Introduce **two narrow, declarative intent CRDs**, each authored by the actor in a workspace it already controls, each reconciled by the security-operator into the org's IDPC in `root:orgs`. This reuses the `APIExportPolicy` delegation pattern rather than inventing a new one.

- **`UpstreamIdentityProviderRegistration`** — authored by the **org owner** in their org workspace (`root:orgs:<org>`). Expresses one upstream IdP. The operator merges it into `spec.upstreamIdentityProviders` of that org's IDPC.
- **`ClientRegistration`** — authored by a **provider operator** in its workspace (`root:providers:<name>`). Expresses one OIDC client for one or more target orgs. The operator merges it into `spec.clients` of each target org's IDPC and delivers the generated secret back to the provider workspace.

Both CRDs are thin: they mirror the existing `IdentityProviderClientConfig` / `UpstreamIdentityProvider` shapes so the operator can merge without translation, and both follow the intent -> trusted-reconcile -> status/finalizer lifecycle of `APIExportPolicy`.

```mermaid
flowchart TD
  subgraph orgws ["root:orgs:&lt;org&gt; (org owner writes)"]
    uipr[UpstreamIdentityProviderRegistration]
  end
  subgraph provws ["root:providers:&lt;name&gt; (provider writes)"]
    cr[ClientRegistration]
    secret[Delivered client_secret Secret]
  end
  secop[security-operator - sole root:orgs writer]
  subgraph protected [root:orgs]
    idpc[IdentityProviderConfiguration]
  end
  uipr -.intent.-> secop
  cr -.intent.-> secop
  secop -->|"merge upstreamIdentityProviders"| idpc
  secop -->|"merge clients"| idpc
  secop -->|"deliver secret"| secret
  idpc -->|existing reconcile| keycloak[Keycloak realm]
</parameter>
```

### 3.1 Why two CRDs instead of one generic CRD

- Each spec is small, self-documenting and validated by a purpose-fit webhook (redirect-URI allowlist for clients; email-domain rules for upstream IdPs).
- RBAC is clean: a provider is granted only `ClientRegistration`, never anything IdP-related; an org owner only `UpstreamIdentityProviderRegistration`.
- It matches `APIExportPolicy`, which reviewers already understand.
- Shared plumbing (merge into IDPC, status, finalizer, secret handling) is factored into one internal package so we can collapse toward a single generic CRD later without an API break.

### 3.2 Reconcile flow (per target org)

For **both** CRDs the security-operator:

1. **Opt-in gate.** Verify the org actually opted into this contribution before writing. Reuse the FGA `bind` check pattern from the rebac-authz-webhook (`handleKCPBindCheck`) and the tuple shape written by `APIExportPolicy`. Never trust the CR's declared target alone.
2. **Merge into IDPC.** `CreateOrPatch` the org IDPC in `root:orgs`, appending the client / upstream IdP. Reuse the existing `mergeManagedClients` idiom so platform-managed clients (`portal`, `kubectl`) and other contributors are never clobbered.
3. **Wait for readiness** via existing IDPC status (`status.managedClients[...]` / `status.managedUpstreamIdentityProviders[...]`).
4. **(ClientRegistration only) Deliver secret.** Copy the generated `client_secret` from `root:orgs` into the provider workspace named by the CR, using the existing secret read/write idiom extended to cross-workspace.
5. **Status + finalizer.** Record managed targets in status; on delete, remove the contributed entry from IDPC spec and delete any delivered secret.

### 3.3 New API types (sketch)

`ClientRegistration` (cluster-scoped, in `root:providers:<name>`):

```go
type ClientRegistrationSpec struct {
    ClientName             string   // must not collide with platform-managed clients
    ClientType             string   // confidential | public
    RedirectURIs           []string // validated against an allowlist (webhook)
    PostLogoutRedirectURIs []string
    AllowPathExpressions   []string // target orgs, e.g. ":root:orgs:*" (APIExportPolicy style)
    SecretDeliveryRef      SecretReference // where to deliver client_secret
}
```

`UpstreamIdentityProviderRegistration` (cluster-scoped, in `root:orgs:<org>`): wraps the existing `UpstreamIdentityProvider` struct verbatim, so no new upstream-IdP semantics are introduced.

Both carry status `{ conditions, managedTargets, ready }`, register in `addKnownTypes`, and regenerate deepcopy.

### 3.4 Reusable building blocks

Most controls already exist; only two pieces are net-new.

- Opt-in gate: FGA `bind` check pattern (`handleKCPBindCheck`) + `APIExportPolicy` tuple shape. Net-new: a `Check` helper in the security-operator's `internal/fga` (today write/list only).
- Validation webhook: `mcruntime.NewWebhookManagedBy(...).WithValidator(...)` scaffold from the IDPC validation webhook; deny-list idiom from `RealmDenyList` / `OrganizationNameDenyList`; `NormalizeEmailDomains`. Net-new: redirect-URI validation (does not exist today — `redirectUris` currently flow straight to Keycloak).
- Secret delivery: read/write/delete idiom from `createOrUpdateSecret` / `readRegistrationAccessToken`; cleanup via finalizer. Net-new: cross-workspace copy into the provider workspace.
- Reconciler + `Account`-Ready watch: model on `apiexportpolicy_controller.go` and `apiexportpolicy.go`; wire in `cmd/system.go`.

### 3.5 Authorization & wiring

- Provider gets create/update on `ClientRegistration` only, in its own workspace; org owner gets `UpstreamIdentityProviderRegistration` only, in the org workspace. No `root:orgs` grants.
- The security-operator watches CRs across workspaces (kcp multicluster), so its watch set extends to provider and org workspaces to observe the intent CRs.
- Generate CRDs + kcp `APIResourceSchema`, add to the `core.platform-mesh.io` APIExport in both `platform-mesh` and `helm-charts` (schema-hash sync), and make both APIs bindable in the relevant workspaces.

---

## 4. Alternatives considered

### 4.1 Option 2 — one generic "IDPC contribution" CRD

A single CRD with a type discriminator (`spec.type: client | upstreamIdentityProvider`) and a union payload. One controller, one merge path, one status shape.

- **For:** no drift between two near-identical reconcile loops, and a third contribution type later becomes a schema change rather than a new CRD plus `APIResourceSchema` plus APIExport sync across two repos.
- **Against — validation.** A union spec can't be validated structurally. "Required for clients, forbidden for upstream IdPs" turns into CEL or webhook branching, so the schema stops being self-documenting and the guarantees move into code.
- **Against — authorization.** This is the deciding one. With two CRDs the FGA model itself states that a provider can only ever contribute clients and an org owner only upstream IdPs. With one CRD, write access is write access, and the type restriction has to be re-checked in the webhook or operator. Authorization by code path is weaker than authorization by model, and harder to review.
- **Verdict:** consolidation target, not a starting point. Revisit once the shared internal package (3.4) exists and if per-type relations on a single object type turn out to be cleanly expressible in FGA.

### 4.2 Option 3 — portal/API for org owners + provider CRD

Org owners never author a CR; the portal calls a backend (iam-service or graphql-gateway) that performs the privileged write. Providers still get `ClientRegistration`.

- The stated motivation — org owners work in the portal, not kubectl — is **already satisfied by Option 1**. The GraphQL gateway exposes CRs in the org workspace generically, so `UpstreamIdentityProviderRegistration` *is* the portal path. The UX goal needs no extra project.
- What remains is a second privileged writer to `root:orgs`. Gateway or iam-service would hold credentials for the protected workspace, putting it in the same blast radius as the security-operator, and duplicating merge logic the operator has to implement anyway.
- Two writers on one IDPC also costs the delete story. With an intent CR, removal is a finalizer on an object that has an owner and an audit trail; with a mutation it's an API call leaving no durable record of who contributed what.
- **Verdict:** the useful half is already in Option 1; the remainder is rejected on the same grounds as Option 4.

### 4.3 Option 4 — scoped direct writes to IDPC in `root:orgs`

Grant org owners a narrow permission to patch only `spec.upstreamIdentityProviders` on their own org's IDPC.

- **Field-level authorization doesn't exist.** kcp and Kubernetes authorize verbs on objects, not on subtrees. Enforcing "only this field" needs an admission webhook diffing old against new, which makes authorization depend on webhook availability: `failurePolicy: Fail` breaks every IDPC write including the operator's when the webhook is down, `Ignore` fails open.
- **Tenants get a handle in `root:orgs`,** which holds every org's IDPC alongside the stored `client_secret` and `registration_access_token` values. Any error in the scoping is cross-tenant by construction rather than contained to one org.
- **The sole-writer model goes away.** Operator reconcile and tenant patch race on one object, and once entries are no longer all operator-authored there is no reliable way to tell tenant contributions from platform ones on delete. `mergeManagedClients` works because contributions have provenance; direct writes erase it.
- **No secret-delivery path** for the `ClientRegistration` half of the problem.
- **Verdict:** rejected. Unvalidated tenant input would land directly in the object the operator treats as trusted, with no type boundary in between — it removes the boundary the rest of this RFC exists to preserve.

This RFC proposes Option 1 as the shippable step that can evolve into Option 2 once the shared internal package makes consolidation cheap.

---

## 5. Open questions

1. **Opt-in signal.** Is FGA `bind` (org "may consume this provider") sufficient, or do we also require a provider tenant CR in the org workspace before a client is issued? (Both presented for discussion.)
2. **Redirect-URI allowlist.** Leaning toward a strict per-registration prefix allowlist validated at admission. Source of the allowed prefixes (provider metadata vs org config vs explicit CR field) to be decided.
3. **Wildcard targeting (`:root:orgs:*`).** Allowed for any provider with a bind, platform-granted only, or explicit org lists only? Depends on the strength of Q1.
4. **Secret rotation.** Reconciler re-delivers on rotation and the provider re-reads the delivered Secret (not a one-shot copy). Contract to be confirmed.

---

## 6. Out of scope

- OpenBao-side operator logic (its tenant CR, auth-engine config, `bound_audiences`).
- Portal UI for either registration flow (relevant to Option 3).

---

## 7. Decision / next steps

- Agree on Option 1 and the open questions in refinement.
- Record an ADR once accepted (`platform-mesh/contrib/adr/`).
- Create an implementation epic; post a reply on backlog#300 steering OpenBao to `ClientRegistration`.
