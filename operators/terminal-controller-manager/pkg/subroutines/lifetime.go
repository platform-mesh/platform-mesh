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

package subroutines

import (
	"context"
	"errors"
	"time"

	"go.platform-mesh.io/apis/terminal/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	LifetimeSubroutineName = "LifetimeSubroutine"
)

// LifetimeSubroutine manages terminal lifetime and triggers deletion when expired
type LifetimeSubroutine struct {
	mgr      mcmanager.Manager
	lifetime time.Duration
}

func NewLifetimeSubroutine(mgr mcmanager.Manager, lifetime time.Duration) *LifetimeSubroutine {
	// Default to 2h if invalid
	if lifetime <= 0 {
		lifetime = 2 * time.Hour
	}

	return &LifetimeSubroutine{
		mgr:      mgr,
		lifetime: lifetime,
	}
}

func (r *LifetimeSubroutine) GetName() string {
	return LifetimeSubroutineName
}

func (r *LifetimeSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	instance := obj.(*v1alpha1.Terminal)
	log := logger.LoadLoggerFromContext(ctx)

	// Check if terminal has exceeded its lifetime
	terminalAge := time.Since(instance.CreationTimestamp.Time)
	if terminalAge > r.lifetime {
		log.Info().
			Str("terminalName", instance.Name).
			Dur("age", terminalAge).
			Dur("lifetime", r.lifetime).
			Msg("terminal exceeded lifetime, triggering deletion")

		// Delete the terminal CR - this will trigger finalization
		if instance.DeletionTimestamp == nil {
			clusterName, ok := mccontext.ClusterFrom(ctx)
			if !ok {
				return subroutines.OK(), errors.New("cluster name not found in context")
			}
			cluster, err := r.mgr.GetCluster(ctx, clusterName)
			if err != nil {
				return subroutines.OK(), err
			}
			if err := cluster.GetClient().Delete(ctx, instance); err != nil && !kerrors.IsNotFound(err) {
				return subroutines.OK(), err
			}
		}
		return subroutines.Stop("terminal exceeded configured lifetime"), nil
	}

	return subroutines.OK(), nil
}

var _ subroutines.Processor = (*LifetimeSubroutine)(nil)
