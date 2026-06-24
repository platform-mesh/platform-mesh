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

package main

// This file holds the release component registry — the single place to edit
// when adding, removing or reordering a release line. To add a component:
//   1. append it to componentOrder (in dependency order — see below),
//   2. add its tag prefix + trigger summary to components,
//   3. if it's a go-gettable library other modules import, add it to
//      libraryComponents so consumers get a `task bump-deps` hint,
//   4. add a row to the usage() table in main.go.

// component describes a release line: its tag prefix (the version is appended
// directly, e.g. prefix "apis/v" + "0.0.1" = "apis/v0.0.1") and a one-line
// summary of what cutting the tag sets in motion — shown in the plan so a
// dry-run makes the downstream effect obvious. Order in componentOrder matters
// for `all`.
type component struct {
	prefix   string
	triggers string
}

// componentOrder is the order `all` releases in: a module is tagged before
// anything that `require`s it, so the published version exists when consumers
// are bumped. golang-commons and subroutines are leaf libraries; apis depends
// on both; the operators and services depend on apis.
var componentOrder = []string{
	"golang-commons",
	"subroutines",
	"apis",
	"account-operator",
	"backup-operator",
	"extension-manager-operator",
	"iam-service",
	"kcp-migration-operator",
	"kubernetes-graphql-gateway",
	"resource-sharding-operator",
	"search-operator",
	"search-service",
	"security-operator",
	"terminal-controller-manager",
	"rebac-authz-webhook",
	"virtual-workspaces",
}

var components = map[string]component{
	"golang-commons":              {"golang-commons/v", "go-gettable module tag for go.platform-mesh.io/golang-commons (no image)"},
	"subroutines":                 {"subroutines/v", "go-gettable module tag for go.platform-mesh.io/subroutines (no image)"},
	"apis":                        {"apis/v", "go-gettable module tag for go.platform-mesh.io/apis (no image)"},
	"account-operator":            {"account-operator/v", "account-operator.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
	"backup-operator":             {"backup-operator/v", "backup-operator.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
	"extension-manager-operator":  {"extension-manager-operator/v", "extension-manager-operator.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
	"iam-service":                 {"iam-service/v", "iam-service.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
	"kcp-migration-operator":      {"kcp-migration-operator/v", "kcp-migration-operator.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
	"kubernetes-graphql-gateway":  {"kubernetes-graphql-gateway/v", "kubernetes-graphql-gateway.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
	"resource-sharding-operator":  {"resource-sharding-operator/v", "resource-sharding-operator.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
	"search-operator":             {"search-operator/v", "search-operator.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
	"search-service":              {"search-service/v", "search-service.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
	"security-operator":           {"security-operator/v", "security-operator.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
	"terminal-controller-manager": {"terminal-controller-manager/v", "terminal-controller-manager.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
	"rebac-authz-webhook":         {"rebac-authz-webhook/v", "rebac-authz-webhook.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
	"virtual-workspaces":          {"virtual-workspaces/v", "virtual-workspaces.yml: builds + signs the image, cuts a GitHub release, bumps the chart, publishes SBOM + signed OCM component"},
}

// libraryComponents are go-gettable modules other modules in the repo import.
// Tagging one only moves the tag; pointing the consumers at the new version is
// a separate, reviewable commit. After a successful release we print the exact
// `task bump-deps` invocation instead of mutating go.mod files behind the
// user's back.
var libraryComponents = map[string]bool{
	"golang-commons": true,
	"subroutines":    true,
	"apis":           true,
}
