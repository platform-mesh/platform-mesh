# pm

`pm start` runs Platform Mesh as one process, the way `kcp start` runs kcp:
an embedded kcp with embedded etcd, an OIDC issuer, OpenFGA, and the Platform
Mesh components as goroutines. No Kubernetes cluster. The portal and its two
micro frontends are Node.js and static sites, so they run as docker
containers that the process starts and stops.

```sh
go build -o bin/pm .          # from cmd/pm
bin/pm start                  # state goes to ./.pm
```

Open http://localhost:3000, onboard an organization and switch to it.

## What runs

| Component | Source | Notes |
|---|---|---|
| kcp | `github.com/kcp-dev/kcp` v0.33.5 | one shard, embedded etcd, `https://localhost:6443`; every `kcp start` flag works |
| mock OIDC issuer | `github.com/oauth2-proxy/mockoidc` | in place of Keycloak, see below |
| OpenFGA | `github.com/openfga/openfga` v1.22.1 | SQLite file in the state directory, gRPC on `--openfga-port` (8081) |
| bootstrap | `pkg/bootstrap` | workspaces, APIExports, WorkspaceTypes, webhook configurations, content |
| account-operator | `operators/account-operator` | controllers and admission webhooks (`--account-webhook-port`, 9443) |
| pm-openfga-operator | `operators/pm-openfga-operator` | all five roles |
| pm-openfga-authz-webhook | `services/pm-openfga-authz-webhook` | kcp's authorization webhook (`--authz-webhook-port`, 9444) |
| iam-service | `services/iam-service` | without a user directory, see below |
| kubernetes-graphql-gateway | `services/kubernetes-graphql-gateway` | listener and gateway (`--graphql-port`, 8080) |
| extension-manager-operator | `operators/extension-manager-operator` | resolves ContentConfigurations |
| virtual-workspaces | extension-manager and marketplace | one API server (`--virtual-workspaces-port`, 6445) behind kcp's `/services/...` paths |
| ingress | `pkg/components/ingress.go` | the one address browsers use (`--portal-port`, 3000), routing by path as a cluster's Gateway does |
| portal, iam-ui, marketplace-ui | docker containers | `--portal-image`, `--iam-ui-image`, `--marketplace-ui-image`; skipped without docker |

`--components` selects a subset. Not included: Keycloak and its operator
(replaced by the issuer), the terminal feature (it needs a cluster to run
pods in), kro-composition-operator and resource-broker (no installation
profile deploys them).

## Logging in

The issuer has no login page and no passwords: every login is
`--mock-oidc-email` (or whoever the request names with `login_hint`). Anyone
who can reach the port is anyone. It is for development only.

Users have no rights of their own: kcp asks the authorization webhook, which
answers from OpenFGA. Whoever onboards an organization owns it; others get in
when an owner gives them a role on the Members page. Without the webhook
(`--components` without it) every user is put in kcp's admin group instead.

```sh
kubectl --server https://localhost:6443/clusters/root:orgs --insecure-skip-tls-verify \
  --token "$(bin/pm token)" get accounts
bin/pm token --email bob@example.com
```

`.pm/admin.kubeconfig` is the kcp admin. kcp renews its token on every start.

## How it differs from an installation

- **No user directory.** iam-service runs with `--idm-provider=none`: a user
  is the e-mail address of their token. A role given to an address takes
  effect when someone logs in with it; there are no invitations and no names.
- **http, on localhost.** Organizations are `http://<org>.localhost:3000`.
  The content manifests link to the micro frontends with `https`; the
  bootstrap rewrites those links.
- **One kcp identity for the components.** Each uses the kcp admin config
  instead of a kubeconfig of its own.
- **The core authorization model is a copy** of `coreModule` in
  `charts/pm-openfga-operator/values.yaml` (`pkg/components/coremodule.fga`):
  the chart's value is not importable. Keep the two the same.
- The portal container shares the host's network, which needs Linux,
  OrbStack or Docker Desktop with host networking enabled.

## Layout

This is a module of its own: embedding kcp means building everything against
kcp's Kubernetes fork, which the `replace` block in `go.mod` copies from kcp's
`go.mod`. Bump both together. `go mod tidy` likes to move
`multicluster-runtime` and `multicluster-provider` forward; pin them back to
what the components use.

A component is wired in `pkg/components` from the exported packages of its
module, never through its cobra command: the commands own process-wide state
(signal handlers, logging setup, `os.Exit`) that can exist once per process.
