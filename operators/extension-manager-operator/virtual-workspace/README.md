> [!WARNING]
> This service is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# extension-manager virtual workspace

A kcp virtual workspace that serves the `ContentConfiguration` resources (`ui.platform-mesh.io`) that apply to a workspace, behind the kcp front-proxy at `/services/contentconfigurations`. The portal reads it to build its navigation and extensions.

A list in a workspace returns the ContentConfigurations that have a configuration result and

- live in the workspace itself,
- are published by the providers whose APIExports the workspace has bound, for the workspace's entity type (organization or account), and
- live in the platform's own provider workspace.

## Why it is a module of its own

The ContentConfiguration API belongs to the extension-manager-operator, so the virtual workspace lives in its directory. It is a separate Go module and image because the kcp virtual workspace framework needs the kcp fork of Kubernetes, which the operator does not build against. `pkg/contentconfiguration` holds the workspace; the virtual workspace API server around it (bootstrap, authentication, authorization, path resolution, storage) comes from the shared library [libs/virtual-workspaces](../../../libs/virtual-workspaces).

## Development

```bash
task test
task lint
```

The image is built from the repository root: `task images-extension-manager-virtual-workspace`.
