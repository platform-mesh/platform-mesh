## Overview

# Contributing to platform-mesh
We want to make contributing to this project as easy and transparent as possible.

## Our development process
We use GitHub to track issues and feature requests, as well as accept pull requests.

## Pull requests
You are welcome to contribute with your pull requests. These steps explain the contribution process:

1. Fork the repository and create your branch from `main`.
1. [Add tests](#testing) for your code.
1. If you've changed APIs, update the documentation.
1. Make sure the tests pass. Our github actions pipeline is running the unit and e2e tests for your PR and will indicate any issues.
1. Sign the Developer Certificate of Origin (DCO).

## Testing

> **NOTE:** You should always add tests if you are adding code to our repository.
To let tests run locally, you can use the Taskfile:
```sh
task test
task cover
```

## Generative AI

If you use generative AI tools while preparing a contribution, you remain responsible for the correctness, safety, licensing, and maintainability of the submitted change.

If you use Claude while contributing, do not assume it will automatically pick up this repository's `AGENTS.md`. Explicitly provide or reference `AGENTS.md` at the start of the session so Claude has the repository-specific instructions before it suggests or applies changes.

## Issues
We use GitHub issues to track bugs. Please ensure your description is
clear and includes sufficient instructions to reproduce the issue.

## License
By contributing to platform-mesh, you agree that your contributions will be licensed
under its [Apache-2.0 license](LICENSE).
