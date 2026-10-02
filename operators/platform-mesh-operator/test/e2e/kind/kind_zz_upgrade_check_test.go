//go:build e2e

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

package e2e

import (
	"os"
	"strings"
	"time"

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
)

// TestZZAGiveReconcilerTime keeps the manager alive long enough (past a
// stale-Ready status check returning instantly) for a real informer-driven
// reconcile of the already-existing PlatformMesh to actually run.
func (s *KindTestSuite) TestZZAGiveReconcilerTime() {
	s.logger.Info().Msg("Sleeping to give the reconciler time to process the existing PlatformMesh with new manifests")
	time.Sleep(90 * time.Second)
	s.logger.Info().Msg("Done waiting")
}

// TestZZWaitForExternalManagedProviders is a manual scratch test used to verify
// externally (kubectl apply -k) applied ManagedProviders reach Ready, for the
// pre-/post-upgrade manual verification. Not part of the normal suite; only
// runs anything when E2E_WAIT_MANAGED_PROVIDERS is set.
func (s *KindTestSuite) TestZZWaitForExternalManagedProviders() {
	ctx := s.T().Context()
	names := os.Getenv("E2E_WAIT_MANAGED_PROVIDERS")
	if names == "" {
		s.T().Skip("E2E_WAIT_MANAGED_PROVIDERS not set")
	}
	for _, name := range strings.Split(names, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		s.logger.Info().Msgf("Waiting until ManagedProvider %s reaches Phase=Ready", name)
		var mp pmprovidersv1alpha1.ManagedProvider
		var lastErr error
		s.Require().Eventually(func() bool {
			lastErr = s.client.Get(ctx, types.NamespacedName{Namespace: "platform-mesh-system", Name: name}, &mp)
			if lastErr != nil {
				return false
			}
			return mp.Status.Phase == "Ready"
		}, 15*time.Minute, 5*time.Second, "waiting for ManagedProvider %s to reach Phase=Ready, err=%v phase=%q", name, lastErr, mp.Status.Phase)
		s.logger.Info().Msgf("ManagedProvider %s is Ready", name)
	}
}
