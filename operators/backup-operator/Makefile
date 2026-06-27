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

.PHONY: build manifests generate test test-integration test-e2e-kind test-e2e-kind-real test-e2e-kind-real-sharded test-all cover lint fmt docker-build deploy-kind deploy-etcd-druid teardown-kind teardown-etcd-druid clean help

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
	@echo "    test-e2e-kind      Simulated E2E: uses in-process etcd-druid/CNPG/Velero simulators;"
	@echo "                       no real pods. Fast (~15 min). Deploys etcd-druid CRDs automatically."
	@echo "    test-e2e-kind-real Real E2E: minio + real etcd-druid + real etcdbr — no simulators."
	@echo "                       Actual snapshots written to minio and replayed on restore (~40 min)."
	@echo "    test-e2e-kind-real-sharded"
	@echo "                       Real E2E sharded: mirrors live shard topology across N shards."
	@echo "                       Requires at least one non-e2e Etcd CR in platform-mesh-system."
	@echo "    test-all           Run all test suites in order (unit → simulated e2e → real e2e)."
	@echo "                       Fails fast on first suite failure. Full teardown between e2e suites."
	@echo ""
	@echo "    lint               Run golangci-lint"
	@echo "    fmt                Run gofmt"
	@echo "    cover              Unit tests with coverage report"
	@echo ""
	@echo "  Deploy"
	@echo "    deploy-kind        Build (if changed) and deploy operator to kind cluster"
	@echo "    deploy-etcd-druid  Deploy etcd-druid to kind cluster from local chart"
	@echo "    teardown-kind      Remove operator, CRDs, minio, and etcd-druid from the kind cluster"
	@echo "    teardown-etcd-druid Remove etcd-druid and its CRDs from the kind cluster"
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

## E2E tests (simulated) — uses in-process simulators for etcd-druid/CNPG/Velero; no real pods.
## Build tag: e2e. Fast (~15 min). Requires a running kind cluster. Deploys etcd-druid CRDs automatically.
test-e2e-kind:
	task test-e2e-kind

## Real E2E tests — minio + real etcd-druid + real etcdbr, no simulators.
## Build tag: e2e_real. Actual snapshots written to minio and replayed on restore (~40 min).
test-e2e-kind-real:
	task test-e2e-kind-real

## Sharded real E2E test — discovers live shard count and mirrors topology; builds and deploys first
test-e2e-kind-real-sharded:
	task test-e2e-kind-real -- -run TestRealEtcd_Sharded

## Run all test suites in sequence, failing fast if any suite fails.
## Order: unit → simulated e2e → real e2e (full teardown between e2e suites).
## Each e2e task already tears down its own namespace on exit (success or failure);
## test-all additionally tears down etcd-druid between suites for a clean slate.
test-all:
	@set -e; \
	banner() { echo ""; echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; echo "  $$1"; echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; echo ""; }; \
	START=$$(date +%s); \
	trap 'ELAPSED=$$(( $$(date +%s) - START )); echo ""; echo "  ✗ test-all FAILED  ($$(( ELAPSED / 60 ))m $$(( ELAPSED % 60 ))s — finished at $$(date '"'"'+%Y-%m-%d %H:%M:%S'"'"'))"; echo ""' ERR; \
	echo ""; echo "  started at $$(date '+%Y-%m-%d %H:%M:%S')"; \
	banner "[1/3] unit tests"; \
	task test; \
	banner "[2/3] simulated e2e  (etcd-druid simulators, no real pods)"; \
	task teardown:etcd-druid 2>/dev/null || true; \
	task test-e2e-kind; \
	task teardown:etcd-druid; \
	banner "[3/3] real e2e  (minio + real etcd-druid + real etcdbr)"; \
	task test-e2e-kind-real; \
	task teardown:etcd-druid; \
	ELAPSED=$$(( $$(date +%s) - START )); \
	echo ""; echo "  ✓ all test suites passed  ($$(( ELAPSED / 60 ))m $$(( ELAPSED % 60 ))s — finished at $$(date '+%Y-%m-%d %H:%M:%S'))"; echo ""

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

## Deploy etcd-druid to kind cluster from local chart
deploy-etcd-druid:
	task deploy:etcd-druid

## Remove operator, CRDs, minio, and etcd-druid from the kind cluster
teardown-kind:
	task teardown:kind

## Remove etcd-druid and its CRDs from the kind cluster
teardown-etcd-druid:
	task teardown:etcd-druid

## Remove build artefacts and generated binaries
clean:
	rm -rf bin/
	rm -f cover.out
