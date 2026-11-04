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
	"path/filepath"
	"time"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/internal/metrics"
	"go.platform-mesh.io/subroutines"

	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const FeatureToggleSubroutineName = "FeatureToggleSubroutine"

type KubeconfigBuilder interface {
	Build(ctx context.Context, client ctrlruntimeclient.Client, kcpUrl string) (*rest.Config, error)
}

type defaultKubeconfigBuilder struct{}

func (defaultKubeconfigBuilder) Build(ctx context.Context, client ctrlruntimeclient.Client, kcpUrl string) (*rest.Config, error) {
	return buildKubeconfig(ctx, client, kcpUrl)
}

type FeatureToggleSubroutine struct {
	client             ctrlruntimeclient.Client
	workspaceDirectory string
	kcpUrl             string
	kubeconfigBuilder  KubeconfigBuilder
	kcpHelper          KcpHelper
}

func NewFeatureToggleSubroutine(client ctrlruntimeclient.Client, helper KcpHelper, operatorCfg *config.OperatorConfig, kcpUrl string) *FeatureToggleSubroutine {
	return &FeatureToggleSubroutine{
		client:             client,
		workspaceDirectory: filepath.Join(operatorCfg.WorkspaceDir, "/manifests/features/"),
		kcpUrl:             kcpUrl,
		kubeconfigBuilder:  defaultKubeconfigBuilder{},
		kcpHelper:          helper,
	}
}

func (r *FeatureToggleSubroutine) GetName() string {
	return FeatureToggleSubroutineName
}

func (r *FeatureToggleSubroutine) Finalize(_ context.Context, _ ctrlruntimeclient.Object) (subroutines.Result, error) {
	return subroutines.OK(), nil
}

func (r *FeatureToggleSubroutine) Finalizers(instance ctrlruntimeclient.Object) []string { // coverage-ignore
	return []string{}
}

func (r *FeatureToggleSubroutine) Process(ctx context.Context, runtimeObj ctrlruntimeclient.Object) (res subroutines.Result, err error) {
	start := time.Now()
	defer func() {
		labelResult := "success"
		if err != nil {
			labelResult = "error"
		}
		metrics.SubroutineTotal.WithLabelValues(r.GetName(), labelResult).Inc()
		metrics.SubroutineDuration.WithLabelValues(r.GetName()).Observe(time.Since(start).Seconds())
	}()
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	operatorCfg := pmconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)

	inst := runtimeObj.(*pmcorev1alpha1.PlatformMesh)
	for _, ft := range inst.Spec.FeatureToggles {
		switch ft.Name {
		case "feature-enable-getting-started":
			// Implement the logic to enable the getting started feature
			_, applyErr := r.applyKcpManifests(ctx, inst, operatorCfg, "/feature-enable-getting-started")
			if applyErr != nil {
				log.Error().Err(applyErr).Msg("Failed to apply getting started manifests")
				return subroutines.OK(), applyErr
			}
			log.Info().Msg("Enabled 'Getting started configuration' feature")
		case "feature-enable-marketplace-account":
			// Implement the logic to enable the marketplace feature
			_, applyErr := r.applyKcpManifests(ctx, inst, operatorCfg, "/feature-enable-marketplace-account")
			if applyErr != nil {
				log.Error().Err(applyErr).Msg("Failed to apply marketplace manifests")
				return subroutines.OK(), applyErr
			}
			log.Info().Msg("Enabled 'Marketplace configuration' feature")
		case "feature-enable-marketplace-org":
			// Implement the logic to enable the marketplace feature
			_, applyErr := r.applyKcpManifests(ctx, inst, operatorCfg, "/feature-enable-marketplace-org")
			if applyErr != nil {
				log.Error().Err(applyErr).Msg("Failed to apply marketplace manifests")
				return subroutines.OK(), applyErr
			}
			log.Info().Msg("Enabled 'Marketplace configuration' feature")
		case "feature-accounts-in-accounts":
			_, applyErr := r.applyKcpManifests(ctx, inst, operatorCfg, "/feature-accounts-in-accounts")
			if applyErr != nil {
				log.Error().Err(applyErr).Msg("Failed to apply accounts-in-accounts manifests")
				return subroutines.OK(), applyErr
			}
			log.Info().Msg("Enabled 'Accounts in accounts' feature")
		case "feature-enable-account-iam-ui":
			_, applyErr := r.applyKcpManifests(ctx, inst, operatorCfg, "/feature-enable-account-iam-ui")
			if applyErr != nil {
				log.Error().Err(applyErr).Msg("Failed to apply account-iam-ui manifests")
				return subroutines.OK(), applyErr
			}
			log.Info().Msg("Enabled 'Account IAM UI' feature")
		case "feature-enable-terminal-controller-manager":
			_, applyErr := r.applyKcpManifests(ctx, inst, operatorCfg, "/feature-enable-terminal-controller-manager")
			if applyErr != nil {
				log.Error().Err(applyErr).Msg("Failed to apply terminal-controller-manager manifests")
				return subroutines.OK(), applyErr
			}
			log.Info().Msg("Enabled 'Terminal controller manager' feature")
		case "feature-enable-provider-permissions":
			_, applyErr := r.applyKcpManifests(ctx, inst, operatorCfg, "/feature-enable-provider-permissions")
			if applyErr != nil {
				log.Error().Err(applyErr).Msg("Failed to apply provider-permissions manifests")
				return subroutines.OK(), applyErr
			}
			log.Info().Msg("Enabled 'Provider permissions' feature")
		case "feature-disable-email-verification":
			log.Info().Msg("Enabled 'disable-email-verification' feature")
		case "feature-disable-idp-webhook":
			// This feature toggle should only be used intentionally for testing/development or for disaster recovery.
			// The IDP validating webhook provides important security validation for IdentityProviderConfiguration resources.
			// Production deployments should run with this webhook enabled (default behavior).
			log.Info().Msg("IDP validating webhook is disabled")
		default:
			log.Warn().Str("featureToggle", ft.Name).Msg("Unknown feature toggle")
		}
	}

	return subroutines.OK(), nil
}

func (r *FeatureToggleSubroutine) applyKcpManifests(
	ctx context.Context,
	inst *pmcorev1alpha1.PlatformMesh,
	operatorCfg config.OperatorConfig,
	kcpDir string,
) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	// Implement the logic to enable the getting started feature
	log.Info().Str("Directory", kcpDir).Msg("Applying KCP manifests for feature toggle")

	// Build kcp kubeconfig
	cfg, err := buildKubeconfig(ctx, r.client, r.kcpUrl)
	if err != nil {
		log.Error().Err(err).Msg("Failed to build kubeconfig")
		return subroutines.OK(), gcerrors.Wrap(err, "Failed to build kubeconfig")
	}

	dir := r.workspaceDirectory + kcpDir

	tplValues := make(map[string]any)
	for k, v := range getExposureParams(inst, operatorCfg.Exposure).templateVars(operatorCfg.KCP) {
		tplValues[k] = v
	}
	err = ApplyDirStructure(ctx, dir, "root", cfg, tplValues, inst, r.kcpHelper, operatorCfg.Subroutines.Deployment.IgnoredResources)
	if err != nil {
		log.Err(err).Msg("Failed to apply dir structure")
		return subroutines.OK(), gcerrors.Wrap(err, "Failed to apply dir structure")
	}

	return subroutines.OK(), nil
}
