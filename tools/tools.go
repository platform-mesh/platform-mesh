//go:build tools

/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Pins tool dependencies so `go mod tidy` keeps them independently of
// build graph changes.
// The tools Taskfile.yaml derives versions of tools that have to move
// with the used libraries via the go dependency graph,
package tools

import (
	_ "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	_ "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/kube-openapi/pkg/validation/spec"
	_ "sigs.k8s.io/controller-runtime/pkg/manager"
	_ "sigs.k8s.io/controller-tools/pkg/loader"
)
