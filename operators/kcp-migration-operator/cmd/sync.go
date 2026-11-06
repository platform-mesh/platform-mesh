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

package cmd

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"go.platform-mesh.io/kcp-migration-operator/pkg/app"
	"go.platform-mesh.io/kcp-migration-operator/pkg/config"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

var syncCfg config.SyncConfig
var syncConfigPath string

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Run in sync mode to migrate resources to kcp",
	Long: `Sync mode watches source resources and synchronizes them to kcp workspaces.

There are two modes of operation:

1. Single-resource mode (via flags):
   Sync a single resource type using command-line flags.

2. Multi-resource mode (via config file):
   Sync multiple resource types using a YAML configuration file.
   Use --config to specify the path to the config file.

Example (single-resource):
  kcp-migration-operator sync \
    --migration-name=my-migration \
    --source-api-version=fabric.foundation.sap.com/v1alpha1 \
    --source-kind=Account \
    --source-namespace=account-2bzns \
    --target-workspace-expression="root:orgs:sap" \
    --template-path=.templates/account.yaml

Example (multi-resource):
  kcp-migration-operator sync --config=config/sync-config.yaml`,
	Run: RunSync,
}

func init() {
	syncCfg = config.NewSyncConfig()
	syncCmd.Flags().StringVar(&syncConfigPath, "config", syncConfigPath, "Path to YAML config file for multi-resource sync")
	syncCfg.AddFlags(syncCmd.Flags())
}

func RunSync(_ *cobra.Command, _ []string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info().Msg("Received shutdown signal")
		cancel()
	}()

	// Check if config file is specified for multi-resource mode
	if syncConfigPath != "" {
		runMultiSync(ctx, syncConfigPath)
		return
	}

	// Single-resource mode via flags
	runSingleSync(ctx)
}

// runSingleSync runs the sync controller for a single resource type using CLI flags
func runSingleSync(ctx context.Context) {
	// Load template from file if template-path is specified
	if syncCfg.Transform.TemplatePath != "" {
		templateBytes, err := os.ReadFile(syncCfg.Transform.TemplatePath)
		if err != nil {
			log.Fatal().Err(err).Str("path", syncCfg.Transform.TemplatePath).Msg("failed to read template file")
		}
		syncCfg.Transform.Template = string(templateBytes)
		log.Info().Str("path", syncCfg.Transform.TemplatePath).Msg("loaded template from file")
	}

	// Load template from ConfigMap if configmap-name is specified
	if syncCfg.Transform.ConfigMapName != "" && syncCfg.Transform.Template == "" {
		log.Info().
			Str("configMapName", syncCfg.Transform.ConfigMapName).
			Str("configMapKey", syncCfg.Transform.ConfigMapKey).
			Msg("loading template from ConfigMap")

		// Create a temporary client to read the ConfigMap
		inClusterConfig := ctrl.GetConfigOrDie()
		tmpClient, err := ctrlruntimeclient.New(inClusterConfig, ctrlruntimeclient.Options{Scheme: scheme})
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create client for ConfigMap lookup")
		}

		cm := &corev1.ConfigMap{}
		cmKey := syncCfg.Transform.ConfigMapKey
		if cmKey == "" {
			cmKey = "template.yaml"
		}

		// Get namespace from environment or default to platform-mesh-system
		namespace := os.Getenv("POD_NAMESPACE")
		if namespace == "" {
			namespace = "platform-mesh-system"
		}

		if err := tmpClient.Get(context.Background(), ctrlruntimeclient.ObjectKey{
			Namespace: namespace,
			Name:      syncCfg.Transform.ConfigMapName,
		}, cm); err != nil {
			log.Fatal().Err(err).
				Str("configMapName", syncCfg.Transform.ConfigMapName).
				Str("namespace", namespace).
				Msg("failed to get ConfigMap")
		}

		template, ok := cm.Data[cmKey]
		if !ok {
			log.Fatal().
				Str("configMapName", syncCfg.Transform.ConfigMapName).
				Str("key", cmKey).
				Msg("template key not found in ConfigMap")
		}

		syncCfg.Transform.Template = template
		log.Info().
			Str("configMapName", syncCfg.Transform.ConfigMapName).
			Str("key", cmKey).
			Msg("loaded template from ConfigMap")
	}

	log.Info().
		Str("migrationName", syncCfg.MigrationName).
		Str("migrationNamespace", syncCfg.MigrationNamespace).
		Str("sourceAPIVersion", syncCfg.Source.APIVersion).
		Str("sourceKind", syncCfg.Source.Kind).
		Str("sourceNamespace", syncCfg.Source.Namespace).
		Strs("sourceLabelSelectors", syncCfg.Source.LabelSelectors).
		Int("maxWorkers", syncCfg.Performance.MaxWorkers).
		Msg("starting sync controller (single-resource mode)")

	// Validate required config
	if syncCfg.MigrationName == "" || syncCfg.Source.APIVersion == "" || syncCfg.Source.Kind == "" {
		log.Fatal().Msg("migration-name, source-api-version, and source-kind are required")
	}

	deps := app.Deps{Log: log, KCPConfig: loadKCPConfig(syncCfg.KCPKubeconfigPath)}
	if err := app.RunSync(ctx, loadSourceConfig(syncCfg.SourceKubeconfigPath), []*config.SyncConfig{&syncCfg}, deps); err != nil {
		log.Fatal().Err(err).Msg("problem running sync manager")
	}
}

// runMultiSync runs multiple sync controllers based on a YAML configuration file
func runMultiSync(ctx context.Context, configPath string) {
	log.Info().Str("config", configPath).Msg("starting sync controller (multi-resource mode)")

	// Read config file
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatal().Err(err).Str("path", configPath).Msg("failed to read config file")
	}

	// Parse config
	var multiCfg config.MultiSyncConfig
	if err := yaml.Unmarshal(configBytes, &multiCfg); err != nil {
		log.Fatal().Err(err).Msg("failed to parse config file")
	}

	// Validate config
	if len(multiCfg.Resources) == 0 {
		log.Fatal().Msg("no resources configured in config file")
	}

	log.Info().
		Str("kcpKubeconfigPath", multiCfg.KCPKubeconfigPath).
		Str("templatesDir", multiCfg.TemplatesDir).
		Int("resourceCount", len(multiCfg.Resources)).
		Msg("loaded multi-sync config")

	cfgs := make([]*config.SyncConfig, 0, len(multiCfg.Resources))
	for _, resCfg := range multiCfg.Resources {
		cfgs = append(cfgs, resourceSyncConfigToSyncConfig(resCfg, multiCfg.TemplatesDir))
	}

	deps := app.Deps{Log: log, KCPConfig: loadKCPConfig(multiCfg.KCPKubeconfigPath)}
	if err := app.RunSync(ctx, loadSourceConfig(multiCfg.SourceKubeconfigPath), cfgs, deps); err != nil {
		log.Fatal().Err(err).Msg("problem running sync manager")
	}
}

// loadSourceConfig returns the source cluster config: the kubeconfig at path if
// set, otherwise the in-cluster config.
func loadSourceConfig(path string) *rest.Config {
	if path == "" {
		log.Info().Msg("using in-cluster config for watching resources")
		return ctrl.GetConfigOrDie()
	}
	restConfig, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		log.Fatal().Err(err).Str("path", path).Msg("failed to load source kubeconfig")
	}
	log.Info().Str("path", path).Msg("using source kubeconfig for watching resources")
	return restConfig
}

// loadKCPConfig returns the kcp config from the kubeconfig at path, or nil
// (sync to kcp disabled) if path is empty.
func loadKCPConfig(path string) *rest.Config {
	if path == "" {
		return nil
	}
	restConfig, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		log.Fatal().Err(err).Str("path", path).Msg("failed to load kcp kubeconfig")
	}
	log.Info().Str("path", path).Msg("kcp workspace client factory configured")
	return restConfig
}

// resourceSyncConfigToSyncConfig converts a ResourceSyncConfig to a SyncConfig
func resourceSyncConfigToSyncConfig(resCfg config.ResourceSyncConfig, templatesDir string) *config.SyncConfig {
	cfg := &config.SyncConfig{
		MigrationName: resCfg.Name,
		Source:        resCfg.Source,
		Target:        resCfg.Target,
		Transform:     resCfg.Transform,
		Performance:   resCfg.Performance,
	}

	// Set defaults
	if cfg.Performance.MaxWorkers == 0 {
		cfg.Performance.MaxWorkers = 1
	}
	if cfg.Performance.RateLimitResourcesPerSecond == 0 {
		cfg.Performance.RateLimitResourcesPerSecond = 50
	}
	if cfg.Performance.RateLimitBurst == 0 {
		cfg.Performance.RateLimitBurst = 100
	}

	// Handle template loading from file
	if cfg.Transform.Template == "" && cfg.Transform.TemplatePath != "" {
		// Load template from file
		templatePath := cfg.Transform.TemplatePath
		if templatesDir != "" {
			templatePath = filepath.Join(templatesDir, templatePath)
		}
		templateBytes, err := os.ReadFile(templatePath)
		if err != nil {
			log.Fatal().Err(err).
				Str("path", templatePath).
				Str("resource", resCfg.Name).
				Msg("failed to read template file")
		}
		cfg.Transform.Template = string(templateBytes)
		log.Info().
			Str("path", templatePath).
			Str("resource", resCfg.Name).
			Msg("loaded template from file")
	}

	return cfg
}
