# Component entrypoints

Every Platform Mesh component that is a Go program can be started in two
ways: as its own binary, and as a goroutine of a process that hosts several
components (the integration tests, a single-binary Platform Mesh). Both go
through the same exported function. This page says what that function looks
like.

## The shape

```
<component>/
  main.go            calls cmd.Execute()
  cmd/               cobra: flags, process-wide setup, one call into pkg/app
  pkg/config/        the options: a struct, its defaults, AddFlags
  pkg/app/           Run, and the pieces it is made of
  internal/          controllers, handlers
```

```go
// pkg/app
func Run(ctx context.Context, kcpCfg *rest.Config, cfg config.Config, deps Deps) error
```

A component with several roles has one such function per role
(`RunOperator`, `RunInitializer`, ...) in the same package.

## Rules for `Run`

1. **It runs until `ctx` is done and returns an error.** No `os.Exit`,
   `log.Fatal`, `klog.Exit` or `panic` on the way; no signal handler.
   A cancelled context is a nil return.

2. **It takes the kcp rest config as an argument** and does not look for
   one (`ctrl.GetConfig`, `KUBECONFIG`, a path flag). The command loads the
   config and passes it. `Run` copies it before changing anything.

3. **Workspaces are options, not paths of the config.** `Run` works with
   whatever workspace the config points at, including none: it points the
   config where it needs with `golang-commons/kcpconfig`.

   ```go
   restCfg, err := kcpconfig.ForWorkspace(kcpCfg, cfg.KCP.Workspace)
   ```

   The option is `--kcp-workspace`. Its default is empty, which means "the
   workspace the config points at", so a kubeconfig that already points at
   the right workspace keeps working without the flag.

4. **Every listener is an option that can be switched off or moved.**
   Metrics and health bind addresses accept `"0"`. A webhook or HTTP server
   has a host and a port option; a certificate directory, not a path baked
   in.

5. **It leaves the process alone.** No `ctrl.SetLogger`, no
   `logsapi.ValidateAndApply`, no tracing or Sentry initialisation, no
   flags on the global flag sets. These exist once per process and belong
   to the command.

6. **Clients of other services can be injected.** `Deps` carries what the
   caller may already have: the logger, an OpenFGA client. A nil client is
   dialled from the address in the config.

7. **Names that are global to a process are unique to the component.**
   controller-runtime keeps one set of controller names and one metrics
   registry per process: prefix both with the component. Register metrics
   so that a second registration is not a panic.

## The command

The cobra command is what is left: it parses flags into the config, does the
process-wide setup of rule 5, makes the context that ends on a signal, loads
the kcp config and calls `Run`. Anything else in a command is something a
host process cannot use.

## Where the components are

| Component | Entrypoint | Notes |
|---|---|---|
| account-operator | `pkg/app.Run` | |
| extension-manager-operator | `pkg/app.Run` | the validation `server` command is not converted |
| pm-openfga-operator | `pkg/app.RunOperator`, `RunSystem`, `RunModelGenerator`, `RunInitializer`, `RunTerminator` | the initializer and terminator take their workspace from `--workspace-path` |
| pm-openfga-authz-webhook | `pkg/app.Run` | |
| iam-service | `pkg/app.Run` | |
| kubernetes-graphql-gateway | `listener.NewConfig`/`NewServer`/`Run`, `gateway.NewConfig`/`NewServer`/`Run` | older shape of the same idea; takes kubeconfig paths, not a rest config (rule 2) |
| virtual workspaces (marketplace, extension-manager) | `golang-commons/virtual-workspaces/server.Serve` plus each one's `BuildVirtualWorkspace` | takes a kubeconfig path (rule 2) |
| pm-keycloak-operator, platform-mesh-operator, provider-operator, terminal-controller-manager, kro-composition-operator, resource-broker | not converted | |
