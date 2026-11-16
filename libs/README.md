# libs

One Go module, `go.platform-mesh.io/platform-mesh/libs`, for code shared between components of this repository. Each library is a directory:

| Library | Purpose |
|---------|---------|
| [virtual-workspaces](virtual-workspaces) | Server code of the kcp virtual workspaces (bootstrap, authentication, path resolution, storage). |

Components link it with a `replace` directive in their `go.mod` and copy `libs/` in their Dockerfile.

`go.mod` pins the kcp fork of Kubernetes for `virtual-workspaces`. Go ignores the `replace` directives of a dependency, so that choice does not reach importers, but a library that must not build against the fork cannot be tested in this module.

```sh
task lint-libs
task test-libs
```
