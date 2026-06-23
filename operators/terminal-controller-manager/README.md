# Terminal Controller Manager

A Kubernetes controller for managing browser-based terminal sessions to kcp workspaces.

## Overview

Terminal Controller Manager watches `Terminal` custom resources and creates ephemeral pods that provide kubectl access to kcp workspaces. Users connect to these pods via WebSocket (exec API) from a browser-based terminal (xterm.js).

**Inspired by:** [Gardener's terminal-controller-manager](https://github.com/gardener/terminal-controller-manager)

## Architecture

```
Browser (xterm.js) → WebSocket exec → Terminal Pod → kubectl → kcp Workspace
                                           ↑
                            Terminal Controller Manager
                            (watches Terminal CRDs, creates pods)
```

## Documentation

- [Concept Document](docs/CONCEPT.md) - Detailed architecture and design decisions

## Quick Start

```bash
# Build
task build

# Run locally
task run

# Deploy to cluster
task deploy
```

## Status

🚧 **Under Development** - Not ready for production use.

## License

Apache License 2.0
