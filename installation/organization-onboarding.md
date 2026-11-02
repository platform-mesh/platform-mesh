# Organization onboarding

Two paths are available for onboarding a new organization:

- **Portal** (recommended when the Platform Mesh portal is deployed): a
  browser-based flow that requires no `kubectl` access for the end user.
- **kubectl** (always available): a command-line flow for environments without
  a portal, or for operators who prefer direct API access.

## Initial user

A cluster administrator creates the initial administrator in Keycloak's
`welcome` realm and marks its email address as verified. The Keycloak console
is `https://<base-domain>:<external-port>/keycloak/admin/`; its bootstrap
credentials are in the `keycloak-admin` Secret in `platform-mesh-system`.

> kcp rejects tokens whose `email_verified` claim is false.

With `idp.registrationAllowed=true` in the platform-mesh-operator chart, users
can instead register from the login page.

## Portal-based onboarding

### 1. Open the portal

Navigate to the portal URL and log in with the user's credentials:

- **Production**: `https://<base-domain>/`
- **Kind**: `https://<base-domain>:8443/`

For example, with the recommended `base_domain=portal.localhost`, the Kind URL
is `https://portal.localhost:8443/`.

After login the portal shows the **Organization management** panel, on a page
titled "Welcome to the Platform Mesh Portal!". Its organization list is empty
until the first organization is onboarded. If it
instead shows a "Welcome! Let's get started." screen, the user is already
inside an organization — navigate back to the root portal URL to reach the
Organization management panel.

### 2. Create the organization

Enter the organization name in the **Organization name** field and click
**Onboard**. Platform Mesh creates an `Account` resource of type `org` and a
dedicated Keycloak realm for the organization in the background.

Wait for the **Switch** button to become active — this indicates the
organization workspace is ready.

### 3. Switch to the organization

The organization's Keycloak realm, `<org-name>`, has its own user accounts.
Onboarding copies only the creator's username and email from the `welcome`
realm. It does not copy the password, name, or verified-email flag. Instead,
Keycloak emails the creator an invitation link to verify the address and set a
password for the new realm. Use that link before signing in to the
organization; the `welcome` realm password does not work there.

> [!IMPORTANT]
> The invitation email requires SMTP, which is not configured by default. Set
> `server`, `port`, and `fromAddress` under
> `components.services.security-operator.values.idp.smtp` in the
> `platform-mesh-profile` ConfigMap. The platform-mesh-operator chart has no
> values for these settings. Without SMTP, the email is not sent and the
> organization creator cannot sign in. Keycloak logs
> `Failed to send execute actions email: … Invalid sender address 'null'`, and
> the portal does not report an error.

Without SMTP, for example on Kind, a Keycloak administrator prepares the
account instead. In the Keycloak console, select the `<org-name>` realm and
open **Users → `<user-email>`**:

1. On **Details**, turn on **Email verified**, remove **Verify Email** from
   **Required user actions**, and click **Save**.
2. On **Credentials**, click **Set password**, enter a password, leave
   **Temporary** on, and click **Save**. Give this password to the user.

Click **Switch**. The portal redirects to the organization-scoped portal at
`https://<org-name>.<base-domain>/` (Kind: `https://<org-name>.<base-domain>:8443/home/overview`).

The browser is redirected through the new organization's Keycloak realm. For a
first-time login in that realm you will go through several screens in sequence:

1. **Sign in** — enter your email and the password for the `<org-name>` realm.
2. **Update password** — set a new password if the account was pre-created by
   an administrator.
3. **Update profile** — enter first and last name if not yet on the account.
4. **Back to Application** — click the link to return to the portal after
   completing the profile setup.
5. You may be asked to sign in once more after these steps complete.

Work through each screen as it appears. Once all Keycloak setup screens are
done the portal loads the organization home page.

---

## kubectl-based onboarding

[^localhost-cli]: Browsers resolve `*.localhost`, but some command-line
resolvers do not resolve `kcp.api.portal.localhost`. Configure local DNS or an
explicit host entry before using `kubectl` with a `portal.localhost` instance.

### 1. Create an OIDC kubeconfig

The user needs a kubeconfig that authenticates with the `kubectl oidc-login`
plugin. In the example below:

- `<base-domain>` is the Platform Mesh base domain.[^localhost-cli]
- `<external-port>` is `443` for the production example or `8443` for Kind.
- `<oidc-client-id>` is the Keycloak OIDC client ID for the `welcome-kubectl`
  client in the `welcome` realm; find it in the Keycloak admin console under
  **Clients → welcome-kubectl → Settings → Client ID**.
- `<base64-ca-bundle>` is a base64-encoded PEM bundle combining the kcp Root CA
  and the domain certificate CA (both are needed: kcp for the front-proxy TLS,
  the domain CA for the Keycloak OIDC endpoint).

Install the
[`kubectl oidc-login`](https://github.com/int128/kubelogin) plugin with
`kubectl krew install oidc-login`, then confirm that `kubectl oidc-login` is
available. Install [Krew](https://krew.sigs.k8s.io/) first if necessary. Build
the CA bundle and paste the output in place of each `<base64-ca-bundle>`
placeholder below:

```shell
{ kubectl -n platform-mesh-system get secret root-ca \
    -o jsonpath='{.data.ca\.crt}' | base64 -d; \
  kubectl -n platform-mesh-system get secret domain-certificate \
    -o jsonpath='{.data.ca\.crt}' | base64 -d; } | base64 | tr -d '\n'
```

`kubectl oidc-login` connects to Keycloak itself and does not read the
cluster's `certificate-authority-data`. With a private CA, paste the output of
the following command in place of `<base64-domain-ca>`. If the domain
certificate is issued by a publicly trusted CA, remove the
`--certificate-authority-data` argument.

```shell
kubectl -n platform-mesh-system get secret domain-certificate \
  -o jsonpath='{.data.ca\.crt}'
```

Save the following as `user.kubeconfig`; the next steps use that file:

```yaml
apiVersion: v1
kind: Config
clusters:
  - name: base
    cluster:
      certificate-authority-data: <base64-ca-bundle>
      server: https://kcp.api.<base-domain>:<external-port>/clusters/root:orgs
  - name: default
    cluster:
      certificate-authority-data: <base64-ca-bundle>
      server: https://kcp.api.<base-domain>:<external-port>/clusters/root:orgs
  - name: workspace.kcp.io/current
    cluster:
      certificate-authority-data: <base64-ca-bundle>
      server: https://kcp.api.<base-domain>:<external-port>/clusters/root:orgs
  - name: workspace.kcp.io/previous
    cluster:
      certificate-authority-data: <base64-ca-bundle>
      server: https://kcp.api.<base-domain>:<external-port>/clusters/root:orgs
contexts:
  - name: base
    context:
      cluster: base
      user: platform-user
  - name: default
    context:
      cluster: default
      user: platform-user
  - name: workspace.kcp.io/current
    context:
      cluster: workspace.kcp.io/current
      user: platform-user
  - name: workspace.kcp.io/previous
    context:
      cluster: workspace.kcp.io/previous
      user: platform-user
current-context: workspace.kcp.io/current
preferences: {}
users:
  - name: platform-user
    user:
      exec:
        apiVersion: client.authentication.k8s.io/v1beta1
        command: kubectl
        args:
          - oidc-login
          - get-token
          - --certificate-authority-data=<base64-domain-ca>
          - --oidc-issuer-url=https://<base-domain>:<external-port>/keycloak/realms/welcome
          - --oidc-client-id=<oidc-client-id>
          - --oidc-extra-scope=offline_access
          - --oidc-extra-scope=email
          - --oidc-extra-scope=profile
          - --oidc-use-pkce
          - --grant-type=auto
        env: null
        interactiveMode: IfAvailable
        provideClusterInfo: false
```

### 2. Create the organization account

Using the new user's OIDC kubeconfig, apply an `Account` resource. The first
`kubectl` call opens a browser window for the Keycloak login:

```yaml
apiVersion: core.platform-mesh.io/v1alpha1
kind: Account
metadata:
  name: <organization-name>
spec:
  type: org
  displayName: <Organization Display Name>
  creator: <user-email>
```

```shell
kubectl --kubeconfig ./user.kubeconfig apply --filename account.yaml
```

### 3. Verify the account

```shell
kubectl --kubeconfig ./user.kubeconfig wait \
  --for=condition=Ready account/<organization-name> --timeout=10m
kubectl --kubeconfig ./user.kubeconfig get account <organization-name> --output yaml
```

If it times out, inspect the Account conditions; do not patch generated resources.
