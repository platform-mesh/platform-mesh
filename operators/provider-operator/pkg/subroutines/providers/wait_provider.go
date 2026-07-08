/*
Copyright 2026.

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

package providers

import (
	"context"
	"time"

	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	providersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/provider-operator/internal/config"
	pmsubs "go.platform-mesh.io/provider-operator/pkg/subroutines"
)

const (
	WaitProviderSubroutineName  = "WaitProviderSubroutine"
	waitProviderRequeueDuration = 10 * time.Second
)

// WaitProviderSubroutine polls the Provider resource in the kcp workspace
// until status.phase == "Ready", indicating that SA, RBAC, and the kubeconfig
// Secret have been created by the Provider controller.
type WaitProviderSubroutine struct {
	client    client.Client
	kcpHelper pmsubs.KcpHelper
	cfg       *config.OperatorConfig
	kcpUrl    string
}

func NewWaitProviderSubroutine(cl client.Client, kcpHelper pmsubs.KcpHelper, cfg *config.OperatorConfig, kcpUrl string) *WaitProviderSubroutine {
	return &WaitProviderSubroutine{
		client:    cl,
		kcpHelper: kcpHelper,
		cfg:       cfg,
		kcpUrl:    kcpUrl,
	}
}

func (r *WaitProviderSubroutine) GetName() string {
	return WaitProviderSubroutineName
}

func (r *WaitProviderSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	inst := obj.(*providersv1alpha1.ManagedProvider)

	wsPath := providerRefPath(inst)
	provName := providerRefName(inst)

	restCfg, err := pmsubs.BuildKubeconfigFromConfig(r.client, &r.cfg.KCP, r.kcpUrl)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to build kcp admin config")
	}

	scopedClient, err := r.kcpHelper.NewKcpClient(restCfg, wsPath)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to create kcp client for provider workspace %s", wsPath)
	}

	provider := &providersv1alpha1.Provider{}
	if err := scopedClient.Get(ctx, types.NamespacedName{Name: provName}, provider); err != nil {
		if kerrors.IsNotFound(err) {
			log.Info().Str("workspace", wsPath).Str("provider", provName).Msg("Provider not found yet, requeuing")
			inst.Status.Phase = providersv1alpha1.ManagedProviderPhaseWaitingForProvider
			return subroutines.StopWithRequeue(waitProviderRequeueDuration, "Provider not found yet"), nil
		}
		return subroutines.OK(), gcerrors.Wrap(err, "failed to get Provider %s from workspace %s", provName, wsPath)
	}

	if provider.Status.Phase != providersv1alpha1.ProviderPhaseReady {
		log.Info().Str("workspace", wsPath).Str("phase", provider.Status.Phase).Msg("Provider not Ready yet, requeuing")
		inst.Status.Phase = providersv1alpha1.ManagedProviderPhaseWaitingForProvider
		return subroutines.StopWithRequeue(waitProviderRequeueDuration, "waiting for Provider to become Ready"), nil
	}

	log.Info().Str("workspace", wsPath).Msg("Provider is Ready")
	return subroutines.OK(), nil
}

func (r *WaitProviderSubroutine) Finalize(_ context.Context, _ client.Object) (subroutines.Result, error) {
	return subroutines.OK(), nil
}

func (r *WaitProviderSubroutine) Finalizers(_ client.Object) []string {
	return []string{}
}
