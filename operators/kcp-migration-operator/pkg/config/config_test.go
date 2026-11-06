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

package config

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestNewOperatorConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := NewOperatorConfig()

	require.True(t, cfg.Subroutines.ValidateSpec.Enabled)
	require.True(t, cfg.Subroutines.CreateConfigMap.Enabled)
	require.True(t, cfg.Subroutines.CreateChildOperator.Enabled)
	require.True(t, cfg.Subroutines.UpdateStatus.Enabled)

	require.Equal(t, "ghcr.io/platform-mesh/kcp-migration-operator:latest", cfg.ChildOperator.Image)
	require.Equal(t, "100m", cfg.ChildOperator.Resources.CPURequest)
	require.Equal(t, "500m", cfg.ChildOperator.Resources.CPULimit)
	require.Equal(t, "128Mi", cfg.ChildOperator.Resources.MemoryRequest)
	require.Equal(t, "256Mi", cfg.ChildOperator.Resources.MemoryLimit)

	require.Equal(t, "kcp-kubeconfig", cfg.Secrets.KCPKubeconfig)
	require.Equal(t, "source-kubeconfig", cfg.Secrets.SourceKubeconfig)
}

func TestOperatorConfigAddFlagsParsesValues(t *testing.T) {
	t.Parallel()

	cfg := NewOperatorConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--subroutines-validate-spec-enabled=false",
		"--subroutines-create-configmap-enabled=false",
		"--subroutines-create-child-operator-enabled=false",
		"--subroutines-update-status-enabled=false",
		"--child-operator-image=ghcr.io/platform-mesh/kcp-migration-operator:v1.2.3",
		"--child-operator-cpu-request=250m",
		"--child-operator-cpu-limit=1000m",
		"--child-operator-memory-request=256Mi",
		"--child-operator-memory-limit=512Mi",
		"--secrets-kcp-kubeconfig=my-kcp-secret",
		"--secrets-source-kubeconfig=my-source-secret",
	})
	require.NoError(t, err)

	require.False(t, cfg.Subroutines.ValidateSpec.Enabled)
	require.False(t, cfg.Subroutines.CreateConfigMap.Enabled)
	require.False(t, cfg.Subroutines.CreateChildOperator.Enabled)
	require.False(t, cfg.Subroutines.UpdateStatus.Enabled)

	require.Equal(t, "ghcr.io/platform-mesh/kcp-migration-operator:v1.2.3", cfg.ChildOperator.Image)
	require.Equal(t, "250m", cfg.ChildOperator.Resources.CPURequest)
	require.Equal(t, "1000m", cfg.ChildOperator.Resources.CPULimit)
	require.Equal(t, "256Mi", cfg.ChildOperator.Resources.MemoryRequest)
	require.Equal(t, "512Mi", cfg.ChildOperator.Resources.MemoryLimit)

	require.Equal(t, "my-kcp-secret", cfg.Secrets.KCPKubeconfig)
	require.Equal(t, "my-source-secret", cfg.Secrets.SourceKubeconfig)
}

func TestNewSyncConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := NewSyncConfig()

	require.Equal(t, "template.yaml", cfg.Transform.ConfigMapKey)
	require.Equal(t, 1, cfg.Performance.MaxWorkers)
	require.Equal(t, 50, cfg.Performance.RateLimitResourcesPerSecond)
	require.Equal(t, 100, cfg.Performance.RateLimitBurst)
	require.Equal(t, "/etc/kcp/kubeconfig", cfg.KCPKubeconfigPath)
	require.Empty(t, cfg.SourceKubeconfigPath)
}

func TestSyncConfigAddFlagsParsesValues(t *testing.T) {
	t.Parallel()

	cfg := NewSyncConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--migration-name=test-migration",
		"--migration-namespace=platform-mesh-system",
		"--source-api-version=fabric.foundation.sap.com/v1alpha1",
		"--source-kind=Account",
		"--source-namespace=account-a",
		"--source-label-selectors=app=test,env=dev",
		"--target-workspace-expression=root:orgs:test",
		"--target-namespace=default",
		"--template={{ .metadata.name }}",
		"--template-path=/tmp/template.yaml",
		"--template-configmap-name=sync-templates",
		"--template-configmap-key=custom-template.yaml",
		"--kcp-kubeconfig-path=/tmp/kcp.kubeconfig",
		"--source-kubeconfig-path=/tmp/source.kubeconfig",
		"--rate-limit-resources-per-second=10",
		"--rate-limit-burst=20",
		"--max-workers=3",
	})
	require.NoError(t, err)

	require.Equal(t, "test-migration", cfg.MigrationName)
	require.Equal(t, "platform-mesh-system", cfg.MigrationNamespace)
	require.Equal(t, "fabric.foundation.sap.com/v1alpha1", cfg.Source.APIVersion)
	require.Equal(t, "Account", cfg.Source.Kind)
	require.Equal(t, "account-a", cfg.Source.Namespace)
	require.Equal(t, []string{"app=test", "env=dev"}, cfg.Source.LabelSelectors)
	require.Equal(t, "root:orgs:test", cfg.Target.WorkspaceExpression)
	require.Equal(t, "default", cfg.Target.Namespace)
	require.Equal(t, "{{ .metadata.name }}", cfg.Transform.Template)
	require.Equal(t, "/tmp/template.yaml", cfg.Transform.TemplatePath)
	require.Equal(t, "sync-templates", cfg.Transform.ConfigMapName)
	require.Equal(t, "custom-template.yaml", cfg.Transform.ConfigMapKey)
	require.Equal(t, "/tmp/kcp.kubeconfig", cfg.KCPKubeconfigPath)
	require.Equal(t, "/tmp/source.kubeconfig", cfg.SourceKubeconfigPath)
	require.Equal(t, 10, cfg.Performance.RateLimitResourcesPerSecond)
	require.Equal(t, 20, cfg.Performance.RateLimitBurst)
	require.Equal(t, 3, cfg.Performance.MaxWorkers)
}
