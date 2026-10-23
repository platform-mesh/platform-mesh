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

// Package migrations holds idempotent steps that bring a running kcp
// workspace tree in line with the operator's current APIExport/APIBinding shape.
package migrations

import (
	"context"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// KcpClientFactory copies subroutines.KcpHelper's method, avoids an import cycle.
type KcpClientFactory interface {
	NewKcpClient(config *rest.Config, workspacePath string) (ctrlruntimeclient.Client, error)
}

// Matches subroutines.fieldManagerKcpSetup, must stay identical: this is
// the field manager identity server-side apply uses for ownership.
const fieldManagerKcpSetup = "platform-mesh-kcp-setup"

// Deps is what a Step needs per run. It is passed to Run, not to New,
// because Config and Instance change on every reconcile.
type Deps struct {
	KcpHelper KcpClientFactory
	Config    *rest.Config
	Instance  *pmcorev1alpha1.PlatformMesh
}

// Step is one idempotent change, checked on every reconcile.
// A run against already-migrated or never-legacy state must be a no-op.
type Step interface {
	// Name identifies the step in logs and wrapped errors.
	Name() string
	// Run checks for the legacy state this step handles and fixes it up if found.
	Run(ctx context.Context, deps Deps) error
}

// Runner runs a fixed, ordered list of steps.
type Runner struct {
	steps []Step
}

// New returns a Runner with every step the operator currently ships, in order.
func New() *Runner {
	return &Runner{steps: []Step{
		fgaAPIExportSplit{},
		providerAPIExportSplit{},
	}}
}

// Run runs every step in order, stopping at the first error. Steps
// must be idempotent so a retry safely restarts from the top.
func (r *Runner) Run(ctx context.Context, deps Deps) error {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("component", "migrations")

	for _, step := range r.steps {
		if err := step.Run(ctx, deps); err != nil {
			return gcerrors.Wrap(err, "migration %s failed", step.Name())
		}
		log.Debug().Str("migration", step.Name()).Msg("migration step checked")
	}

	return nil
}
