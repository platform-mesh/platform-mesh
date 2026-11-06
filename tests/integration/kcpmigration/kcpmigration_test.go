//go:build integration

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

package kcpmigration

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmmigrationv1alpha1 "go.platform-mesh.io/apis/migration/v1alpha1"
	migrationconfig "go.platform-mesh.io/kcp-migration-operator/pkg/config"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func operatorConfig() *migrationconfig.OperatorConfig {
	return &migrationconfig.OperatorConfig{}
}

// waitForPhase waits until the KCPMigration reaches phase and returns it.
func (s *IntegrationSuite) waitForPhase(key ctrlruntimeclient.ObjectKey, phase pmmigrationv1alpha1.Phase) *pmmigrationv1alpha1.KCPMigration {
	got := &pmmigrationv1alpha1.KCPMigration{}
	s.Require().EventuallyWithT(func(c *assert.CollectT) {
		require.NoError(c, s.client.Get(s.ctx, key, got))
		assert.Equal(c, phase, got.Status.Phase)
	}, timeout, interval, "KCPMigration %s did not reach phase %s", key, phase)
	return got
}

func (s *IntegrationSuite) TestKCPMigrationStatusRunning() {
	migration := &pmmigrationv1alpha1.KCPMigration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-migration",
			Namespace: "default",
		},
		Spec: pmmigrationv1alpha1.KCPMigrationSpec{
			Source: pmmigrationv1alpha1.SourceSpec{
				APIVersion: "apps.example.com/v1",
				Kind:       "MyApp",
			},
			Transform: pmmigrationv1alpha1.TransformSpec{
				TargetWorkspace: pmmigrationv1alpha1.WorkspaceExpression{
					Expression: "root:platform-mesh:{{ .Source.metadata.namespace }}",
				},
			},
		},
	}
	s.Require().NoError(s.client.Create(s.ctx, migration))
	s.T().Cleanup(func() { s.NoError(ctrlruntimeclient.IgnoreNotFound(s.client.Delete(s.ctx, migration))) })

	got := s.waitForPhase(ctrlruntimeclient.ObjectKeyFromObject(migration), pmmigrationv1alpha1.PhaseRunning)
	s.Equal(got.Generation, got.Status.ObservedGeneration, "observedGeneration should be set")
}

func (s *IntegrationSuite) TestKCPMigrationInMigrationSystemNamespace() {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "migration-system"}}
	s.Require().NoError(s.client.Create(s.ctx, ns))

	migration := &pmmigrationv1alpha1.KCPMigration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "system-migration",
			Namespace: ns.Name,
		},
		Spec: pmmigrationv1alpha1.KCPMigrationSpec{
			Source: pmmigrationv1alpha1.SourceSpec{
				APIVersion: "apps.example.com/v1",
				Kind:       "ConfigMap",
			},
			Transform: pmmigrationv1alpha1.TransformSpec{
				TargetWorkspace: pmmigrationv1alpha1.WorkspaceExpression{
					Expression: "root:config",
				},
			},
		},
	}
	s.Require().NoError(s.client.Create(s.ctx, migration))
	s.T().Cleanup(func() { s.NoError(ctrlruntimeclient.IgnoreNotFound(s.client.Delete(s.ctx, migration))) })

	s.waitForPhase(ctrlruntimeclient.ObjectKeyFromObject(migration), pmmigrationv1alpha1.PhaseRunning)
}

func (s *IntegrationSuite) TestKCPMigrationNotFound() {
	err := s.client.Get(s.ctx, ctrlruntimeclient.ObjectKey{Namespace: "default", Name: "non-existent"}, &pmmigrationv1alpha1.KCPMigration{})
	s.True(apierrors.IsNotFound(err), "expected NotFound, got %v", err)
}
