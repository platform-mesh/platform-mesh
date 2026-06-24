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

.PHONY: build clean test run help

# Binary name
BINARY=qbrtool

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod

# Build flags
LDFLAGS=-ldflags "-s -w"

# Default target
all: build

## build: Build the binary
build:
	$(GOBUILD) $(LDFLAGS) -o bin/ ./cmd/qbrtool

## build-debug: Build with debug symbols
build-debug:
	$(GOBUILD) -o bin/ ./cmd/qbrtool

## clean: Clean build artifacts
clean:
	$(GOCLEAN)
	rm -rf bin/

## test: Run tests
test:
	$(GOTEST) -v -race ./...

## test-coverage: Run tests with coverage
test-coverage:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

## deps: Download dependencies
deps:
	$(GOMOD) download

## tidy: Tidy dependencies
tidy:
	$(GOMOD) tidy

## vet: Run go vet
vet:
	$(GOCMD) vet ./...

## lint: Run linters (requires golangci-lint)
lint:
	golangci-lint run

## install: Install the binary to $GOPATH/bin
install:
	$(GOCMD) install ./cmd/qbrtool

## run: Run the binary (use ARGS to pass arguments)
run: build
	./bin/$(BINARY) $(ARGS)

## help: Show this help
help:
	@echo "Available targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/ /'

# Example usage targets
## export-q3: Export Q3-2024 items (example)
export-q3: build
	./bin/$(BINARY) export --quarter Q3-2024 --include-archived -f q3-2024.json

## analyze-all: Run all analyzers on exported data (example)
analyze-all: build
	./bin/$(BINARY) analyze -i q3-2024.json --analysis all -f analysis.json
