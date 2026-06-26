# Copyright The Platform Mesh Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

.PHONY: build manifests generate test test-integration test-e2e-kind cover lint fmt docker-build deploy-kind clean help

## Show this help message
help:
	@echo "Available targets:"
	@echo ""
	@echo "  Build"
	@echo "    build              Build the operator binary"
	@echo "    manifests          Generate CRD + RBAC manifests"
	@echo "    generate           Run manifests + deepcopy codegen"
	@echo "    docker-build       Build the Docker image"
	@echo ""
	@echo "  Test"
	@echo "    test               Unit tests  (no cluster, no API server)"
	@echo "    test-integration   Integration tests via envtest (API server in-process, no cluster)"
	@echo "    test-e2e-kind      E2E tests against a running kind cluster (builds + deploys first)"
	@echo ""
	@echo "  Quality"
	@echo "    lint               Run golangci-lint"
	@echo "    fmt                Run gofmt"
	@echo "    cover              Unit tests with coverage report"
	@echo ""
	@echo "  Deploy"
	@echo "    deploy-kind        Build (if changed) and deploy operator to kind cluster"
	@echo "    clean              Remove build artefacts (bin/, cover.out)"

build:
	task build

manifests:
	task manifests

generate:
	task generate

## Unit tests — no cluster or API server required
test:
	task test

## Integration tests — envtest spins up an in-process API server with etcd-druid CRDs; no cluster needed
test-integration:
	task test-e2e

## E2E tests — requires a running kind cluster with etcd-druid; builds and deploys the operator first
test-e2e-kind:
	task test-e2e-kind

cover:
	task cover

lint:
	task lint

fmt:
	task fmt

docker-build:
	task docker-build

## Deploy (or redeploy on source change) the operator to the kind cluster without running tests
deploy-kind:
	task deploy:kind

## Remove build artefacts and generated binaries
clean:
	rm -rf bin/
	rm -f cover.out
