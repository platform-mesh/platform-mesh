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

> **NOTE:** You should always add if you are adding code to our repository.

To let tests run locally, run `go test ./...` in the root directory of the repository.


### Test your change in a locally running platform-mesh instance


```bash
docker build -t account-operator:latest . && \
kind load docker-image account-operator:latest --name=platform-mesh && \
kubectl patch deployment platform-mesh-account-operator -n platform-mesh-system --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/imagePullPolicy", "value": "IfNotPresent"}]' && \
kubectl patch deployment platform-mesh-account-operator -n platform-mesh-system --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/image", "value": "account-operator:latest"}]' && \
kubectl rollout restart deployment platform-mesh-account-operator -n platform-mesh-system && \
kubectl rollout status deployment platform-mesh-account-operator -n platform-mesh-system
```
## Issues
We use GitHub issues to track bugs. Please ensure your description is
clear and includes sufficient instructions to reproduce the issue.

## License
By contributing to platform-mesh, you agree that your contributions will be licensed
under its [Apache-2.0 license](LICENSE).
