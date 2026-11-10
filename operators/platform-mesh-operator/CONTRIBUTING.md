# Contributing to platform-mesh-operator

## General Remarks

Contributions to this project are welcome.

Before contributing:

1. Follow the project [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md).
2. Be prepared to accept the Developer Certificate of Origin (DCO) during the pull request process.
3. If you plan a larger feature or architectural change, open an issue first to confirm it fits the project direction.

## How to Contribute

1. Fork the repository and create a branch from `main`.
2. Make your changes.
3. Add or update tests when behavior changes.
4. Verify your changes locally.
5. Open a pull request and address review feedback.

## Development

Prerequisites: Go, Docker, kubectl, kind, mkcert, and [Task](https://taskfile.dev). For installation details and `PlatformMesh` resource semantics, see [README.md](README.md).

Key commands:

- `task lint` — run formatting and golangci-lint
- `task test` — run the standard local test flow
- `task tilt:e2e` (repository root) — run the end-to-end scenarios in `tests/e2e/platform-mesh-operator` against a Tilt cluster
- `task cover` — run coverage checks
- `task validate` — run lint and tests
- `task manifests` — regenerate CRDs
- `task generate` — regenerate CRDs and deepcopy code after API changes
- `task mockery` — regenerate mocks
- `task build` — build the manager binary
- `task run` — run the operator locally using `.env`
- `task docker-build` — build the container image
- `task docker:kind` — load the image into kind and restart the deployment

## Pull Requests

- Keep pull requests focused and easy to review.
- Update documentation when `PlatformMesh` behavior, installation flow, or manifests change.
- Make sure local verification passes before opening or updating the PR.
- If you change API types, run `task generate` and include the generated output in the same PR.
- Review manifest changes carefully; files under `manifests/` can affect full platform bootstrap behavior.

## Generative AI

If you use generative AI tools while preparing a contribution, you remain responsible for the correctness, safety, licensing, and maintainability of the submitted change.

Follow the org-wide [guideline for AI-generated code contributions](https://github.com/platform-mesh/.github/blob/main/CONTRIBUTING_USING_GENAI.md).

If you use Claude while contributing, do not assume it will automatically pick up this repository's `AGENTS.md`. Explicitly provide or reference `AGENTS.md` at the start of the session so Claude has the repository-specific instructions before it suggests or applies changes.

## License

By contributing, you agree that your contributions will be licensed under the [Apache-2.0 License](LICENSE).
