/*
Copyright 2026 The kcp Authors.

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

// Package controller holds the identity the access virtual workspace runs
// as: a ServiceAccount in the workspace that hosts the APIExport, its token,
// and the RBAC it needs. Keeping the identity in that workspace means the
// server never needs an admin kubeconfig.
package controller

import "embed"

// FS holds the ServiceAccount, its token secret and RBAC.
//
//go:embed *.yaml
var FS embed.FS

// Order is the sequence the assets must be applied in: the ServiceAccount
// before the RBAC that binds it.
var Order = []string{
	"serviceaccount.yaml",
	"rbac.yaml",
}
