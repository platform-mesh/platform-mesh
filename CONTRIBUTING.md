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

## Getting Started

### Prerequisites
- go version v1.24.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy in the local cluster
**Run the platform-mesh locally in the cluster:**

To deploy the platform to kubernetes locally, please refer to the [helm-charts](https://github.com/platform-mesh/helm-charts) repository. 

**Build and push your image to the local kind cluster where the platform-mesh system is deployed:**

For that you can or spot which version is currently being used in the cluster and execute next commands:

```bash
docker build -t <image name which is used in the cluster with the tag> .

kind load docker-image <image name which is used in the cluster with the tag> --name=platform-mesh
```
After it you need to restart security operator's pods and they will fetch a local image automaticly

If you want to use another image name and tag use this approach

```bash
docker build -t security-operator:latest . && \
kind load docker-image security-operator:latest --name=platform-mesh && \
kubectl patch deployment platform-mesh-security-operator -n platform-mesh-system --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/imagePullPolicy", "value": "IfNotPresent"}]' && \
kubectl patch deployment platform-mesh-security-operator -n platform-mesh-system --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/image", "value": "security-operator:latest"}]' && \
kubectl rollout restart deployment platform-mesh-security-operator -n platform-mesh-system && \
kubectl rollout status deployment platform-mesh-security-operator -n platform-mesh-system
```

**Install the CRDs into the cluster:**

```bash
task install
```

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```bash
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```bash
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```bash
task uninstall
```

## Useful commands:
### Run linter
```bash
task lint
```
### Coverage check
```bash
task cover
```
### Run tests
```bash
task test
```
### Check tests and linter
```bash
task validate
```

## Issues
We use GitHub issues to track bugs. Please ensure your description is
clear and includes sufficient instructions to reproduce the issue.

## License
By contributing to platform-mesh, you agree that your contributions will be licensed
under its [Apache-2.0 license](LICENSE).
