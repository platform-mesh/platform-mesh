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
// release has: it lists steps that seed it into the release, and steps that
// validate that it is there and works. The upgrade test calls Seed and
// Validate on the release, and Validate again after the upgrade. The steps
// are run by tests/framework/steps: each is called until it returns nil.
//
// The files:
//
//	scenarios.go     the types, All (the scenarios that run), Seed and Validate
//	tenant.go        the tenant scenario: its names and its steps, in order
//	organization.go  steps of the tenant scenario about the organization
//	invites.go       steps of the tenant scenario about invites and their users
//	account.go       steps of the tenant scenario about the account
//
// To add a scenario, write a file with a function returning it, like Tenant,
// and list it in All.
package scenarios

import (
	"context"
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
}

// All returns the scenarios the upgrade test runs.
//
// ORDER MATTERS little here: the scenarios run in this order, but one that
// fails does not stop the next, so a scenario must not rely on another one.
// What has to happen in sequence belongs into one scenario.
func All() []Scenario {
	return []Scenario{
		Tenant(),
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
