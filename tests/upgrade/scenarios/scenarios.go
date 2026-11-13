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

// Package scenarios holds what the upgrade test (tests/upgrade) expects to
// survive an upgrade of Platform Mesh. A scenario is something a user of the
// release has: it lists steps that seed it into the release, steps that
// validate that it is there and works, and steps that go on working with it
// after the upgrade. The upgrade test calls Seed and Validate on the release,
// and Validate again and then After on the checkout. The steps are run by
// tests/framework/steps: each is called until it returns nil.
//
// The files:
//
//	scenarios.go     the types, All (the scenarios that run), Seed, Validate, After
//
//	tenant.go        scenario: an organization with users, accounts and data
//	roles.go         scenario: roles granted through the IAM service
//	isolation.go     scenario: two organizations that must not see each other
//	offboarding.go   scenario: an organization that is deleted after the upgrade
//	provider.go      scenario: a provider API bound and used by an account
//	platform.go      scenario: what the operator of the installation configured
//	extensions.go    scenario: UI configuration of an account
//
//	organization.go  steps about an organization and its users
//	invites.go       steps about invites
//	account.go       steps about an account and the data in its workspace
//	iam.go           steps about roles, through the IAM service
//	gateway.go       steps that read through the GraphQL gateway
//	identity.go      steps that pin what must not change in an upgrade
//	access.go        how the steps reach the installation: as the admin, as
//	                 a user, over GraphQL
//	state.go         what a scenario remembers from Seed for Validate
//	manifests.go     the objects kept as YAML, in manifests/
//
// A scenario file names what the scenario creates and puts its steps in
// order; the step files hold the steps, as functions of those names, so
// that scenarios share them. To add a scenario, write a file with a function
// returning it, like Tenant, and list it in All.
//
// Seed runs on the release with the Go types of this checkout. So a step
// creates objects and patches them, but never updates an object of Platform
// Mesh through its Go type: that drops every field the release has and the
// checkout does not know (see declare in platform.go).
package scenarios

import (
	"context"
	"net/http"
	"testing"

	"go.platform-mesh.io/platform-mesh/tests/framework/keycloak"
	"go.platform-mesh.io/platform-mesh/tests/framework/steps"

	"k8s.io/client-go/rest"
)

// Environment is the installation the steps work on. The upgrade test
// implements it for the kind cluster it installs Platform Mesh into.
type Environment interface {
	// KCP is the kcp admin config of the installation, without a
	// workspace path: what the tests/framework/tenant functions take.
	KCP(ctx context.Context) (*rest.Config, error)
	// Keycloak is the admin client of the Keycloak of the installation.
	Keycloak(ctx context.Context) (keycloak.Client, error)
	// Kube is the admin config of the cluster Platform Mesh is installed
	// into, where its PlatformMesh object and its secrets are.
	Kube(ctx context.Context) (*rest.Config, error)
	// Portal is the address of the portal, for example
	// https://portal.example.com:8443, and an HTTP client that trusts its
	// certificate. The services of an organization are served under it,
	// on the subdomain of the organization.
	Portal(ctx context.Context) (string, *http.Client, error)
}

// A Step is a step (tests/framework/steps) on an Environment: one thing a
// scenario does to the installation, or expects of it. It does one pass and
// is called again until it returns nil, so it does not wait and has to be
// idempotent.
type Step = steps.Step[Environment]

// A Scenario is something a user of the release has, and expects to find and
// to work after the upgrade.
//
// ORDER MATTERS within a scenario: its steps run in the order they are
// listed, each only once the one before returned nil, so a step can rely on
// what the steps before it left behind. Say why at the place they are
// listed.
type Scenario struct {
	Name string
	// Seed creates the data, on the release. A kept cluster is seeded
	// again by the next run.
	Seed []Step
	// Validate finds the data and checks that it works. It runs on the
	// release, right after Seed, and again after the upgrade, so it must
	// not change the installation, and it must not create what it looks
	// for: what is gone has to fail it.
	Validate []Step
	// After goes on working with the data, on the checkout, once Validate
	// held there: it changes it, adds to it, deletes it, as the user would
	// after the upgrade. This is where the new code has to cope with what
	// the old code wrote. It runs once. What it leaves behind, Seed has
	// to be able to bring back to what Validate expects, for the next
	// run on a kept cluster.
	After []Step
}

// All returns the scenarios the upgrade test runs.
//
// ORDER MATTERS little here: the scenarios run in this order, but one that
// fails does not stop the next, so a scenario must not rely on another one.
// What has to happen in sequence belongs into one scenario.
func All() []Scenario {
	return []Scenario{
		Tenant(),
		Roles(),
		Isolation(),
		Offboarding(),
		Provider(),
		Platform(),
		Extensions(),
	}
}

// Seed runs the seed steps of every scenario against env.
func Seed(t *testing.T, env Environment) {
	t.Helper()
	run(t, env, func(s Scenario) []Step { return s.Seed })
}

// Validate runs the validate steps of every scenario against env.
func Validate(t *testing.T, env Environment) {
	t.Helper()
	run(t, env, func(s Scenario) []Step { return s.Validate })
}

// After runs the after steps of every scenario against env.
func After(t *testing.T, env Environment) {
	t.Helper()
	run(t, env, func(s Scenario) []Step { return s.After })
}

// run runs every scenario as a subtest of its name. A scenario that fails
// does not stop the others.
func run(t *testing.T, env Environment, of func(Scenario) []Step) {
	t.Helper()
	for _, s := range All() {
		t.Run(s.Name, func(t *testing.T) {
			steps.Run(t, env, of(s)...)
		})
	}
}
