> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# Platform-Mesh virtual workspaces

[![OpenSSF Scorecard](https://api.scorecard.dev/projects/go.platform-mesh.io/virtual-workspaces/badge)](https://scorecard.dev/viewer/?uri=go.platform-mesh.io/virtual-workspaces)
![Build Status](https://go.platform-mesh.io/virtual-workspaces/actions/workflows/ci.yml/badge.svg)

## Description

The platform-mesh virtual-workspaces are used to provide custom, deep kcp based extensions for platform-mesh. It prepares and exposed relevant information for depending UIs with means of using an custom apiserver implementation from kubernetes and kcp.

## Features
- Exposes a virtual workspaces to select the right contentconfigurations for a given workspace context
- Exposes a virtual workspaces to expose a `MarketplaceEntry` resource that can be used to feed a marketplace UI

## Getting started

To start the service locally you need some trusted certificates. You can generate them using the following command:

```bash
mkcert -cert-file=.secret/apiserver.crt -key-file=.secret/apiserver.key localhost
```

Also make sure you have `mkcert` installed and trusted its store in your system.

The Kubeconfigs server url needs to point to the **root** of the kcp instance (*not the root cluster*), meaning only the url.

### VSCode Debug config:

```json
{
    // Use IntelliSense to learn about possible attributes.
    // Hover to view descriptions of existing attributes.
    // For more information, visit: https://go.microsoft.com/fwlink/?linkid=830387
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Launch Package",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "${workspaceFolder}/main.go",
            "args": [
                "start",
                "--tls-cert-file=${workspaceFolder}/.secret/apiserver.crt",
                "--tls-private-key-file=${workspaceFolder}/.secret/apiserver.key",
                "--secure-port=6443",
                "--bind-address=0.0.0.0",
                "--authentication-kubeconfig=${workspaceFolder}/.secret/authentication.yaml",
                "--client-ca-file=${workspaceFolder}/.secret/kcp-ca.crt",
                "--authentication-skip-lookup"
            ],
            "envFile": "${workspaceFolder}/.env",
        }
    ]
}
```

The *authentication-kubeconfig* can be a copy of the kubeconfig file you use to access the kcp instance, but with the server-url pointing to the root clusters.

## Releasing

The release is performed automatically through a GitHub Actions Workflow.

All the released versions will be available through access to GitHub (as any other Golang Module).

## Requirements

The virtual-workspaces requires a installation of go. Checkout the [go.mod](go.mod) for the required go version and dependencies.

## Security / Disclosure
If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://go.platform-mesh.io/virtual-workspaces/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Contributing

Please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file in this repository for instructions on how to contribute to platform-mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
