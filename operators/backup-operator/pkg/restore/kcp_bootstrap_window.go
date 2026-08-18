package restore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var kcpOperatorResources = []schema.GroupVersionKind{
	{Group: "operator.kcp.io", Version: "v1alpha1", Kind: "RootShard"},
	{Group: "operator.kcp.io", Version: "v1alpha1", Kind: "Shard"},
}

func (p *PlatformRecoverySubroutine) ensureKCPWebhookDisabled(ctx context.Context, pr *v1alpha1.PlatformRestore, disabledKey, restoredKey string) (bool, error) {
	cm, err := ensureRestoreStateConfigMap(ctx, p.client, pr)
	if err != nil {
		return false, err
	}
	if cm.Data[restoredKey] == string(pr.UID) || cm.Data[disabledKey] == string(pr.UID) {
		ready, err := workloadsReady(ctx, p.client, kcpWebhookConsumers)
		return ready, err
	}

	operator := workloadRef{Namespace: kcpOperatorNamespace, Kind: "Deployment", Name: "kcp-operator"}
	changed, err := operator.scale(ctx, p.client, 0)
	if err != nil {
		return false, fmt.Errorf("scale down KCP operator: %w", err)
	}
	if changed {
		return false, nil
	}
	stopped, err := operator.ready(ctx, p.client)
	if err != nil || !stopped {
		return false, err
	}

	patchBase := cm.DeepCopy()
	for _, workload := range kcpWebhookConsumers {
		arg, changed, err := p.removeKCPWebhookArgument(ctx, workload)
		if err != nil {
			return false, err
		}
		if changed {
			cm.Data[kcpWebhookArgumentPrefix+workload.Name] = arg
		}
	}
	cm.Data[disabledKey] = string(pr.UID)
	if err := p.client.Patch(ctx, cm, ctrlruntimeclient.MergeFrom(patchBase)); err != nil {
		return false, fmt.Errorf("record KCP webhook bootstrap state: %w", err)
	}
	return false, nil
}

func (p *PlatformRecoverySubroutine) ensureKCPWebhookRestored(ctx context.Context, pr *v1alpha1.PlatformRestore, disabledKey, restoredKey string) (bool, error) {
	cm, err := ensureRestoreStateConfigMap(ctx, p.client, pr)
	if err != nil {
		return false, err
	}
	if cm.Data[disabledKey] != string(pr.UID) {
		return true, nil
	}

	for _, workload := range kcpWebhookConsumers {
		if err := p.restoreKCPWebhookArgument(ctx, workload, cm.Data[kcpWebhookArgumentPrefix+workload.Name]); err != nil {
			return false, err
		}
	}
	operator := workloadRef{Namespace: kcpOperatorNamespace, Kind: "Deployment", Name: "kcp-operator"}
	operatorReplicas := int32(1)
	if remembered, ok := rememberedReplicas(cm, operator); ok && remembered > 0 {
		operatorReplicas = remembered
	}
	changed, err := operator.scale(ctx, p.client, operatorReplicas)
	if err != nil {
		return false, fmt.Errorf("scale up KCP operator: %w", err)
	}

	if cm.Data[restoredKey] != string(pr.UID) {
		patchBase := cm.DeepCopy()
		cm.Data[restoredKey] = string(pr.UID)
		if err := p.client.Patch(ctx, cm, ctrlruntimeclient.MergeFrom(patchBase)); err != nil {
			return false, fmt.Errorf("record KCP webhook bootstrap restoration: %w", err)
		}
		return false, nil
	}
	if changed {
		return false, nil
	}

	ready, err := operator.ready(ctx, p.client)
	if err != nil {
		return false, fmt.Errorf("check KCP operator after webhook bootstrap restoration: %w", err)
	}
	return ready, nil
}

func (p *PlatformRecoverySubroutine) removeKCPWebhookArgument(ctx context.Context, workload workloadRef) (string, bool, error) {
	var deployment appsv1.Deployment
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: workload.Namespace, Name: workload.Name}, &deployment); err != nil {
		return "", false, err
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return "", false, fmt.Errorf("KCP deployment %s has no containers", workload.Name)
	}
	container := &deployment.Spec.Template.Spec.Containers[0]
	for index, arg := range container.Args {
		if !strings.HasPrefix(arg, "--authorization-webhook-config-file=") {
			continue
		}
		patchBase := deployment.DeepCopy()
		container.Args = append(container.Args[:index], container.Args[index+1:]...)
		if err := p.client.Patch(ctx, &deployment, ctrlruntimeclient.MergeFrom(patchBase)); err != nil {
			return "", false, fmt.Errorf("disable KCP authorization webhook for %s: %w", workload.Name, err)
		}
		return arg, true, nil
	}
	return "", false, nil
}

func (p *PlatformRecoverySubroutine) restoreKCPWebhookArgument(ctx context.Context, workload workloadRef, argument string) error {
	if argument == "" {
		return nil
	}
	var deployment appsv1.Deployment
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: workload.Namespace, Name: workload.Name}, &deployment); err != nil {
		return err
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("KCP deployment %s has no containers", workload.Name)
	}
	container := &deployment.Spec.Template.Spec.Containers[0]
	for _, arg := range container.Args {
		if arg == argument {
			return nil
		}
	}
	patchBase := deployment.DeepCopy()
	container.Args = append(container.Args, argument)
	return p.client.Patch(ctx, &deployment, ctrlruntimeclient.MergeFrom(patchBase))
}

// ensureReBACAvailability starts ReBAC before KCP resumes normal webhook
// authorization and waits until endpoint discovery can be served.
func (p *PlatformRecoverySubroutine) ensureReBACAvailability(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (recoveryWait, error) {
	log := logger.LoadLoggerFromContext(ctx)
	webhook := platformDeployment("rebac-authz-webhook")
	changed, err := webhook.scale(ctx, p.client, 1)
	if err != nil {
		return recoveryWait{}, fmt.Errorf("start ReBAC authorization webhook: %w", err)
	}
	if changed {
		log.Info().Str("step", "rebac-webhook-availability").Msg("started ReBAC authorization webhook before KCP API claim recovery")
		return recoveryWait{5 * time.Second, "waiting for ReBAC authorization webhook to start"}, nil
	}
	changed, err = restartDeployment(ctx, p.client, webhook, rebacBootstrapRestartAnnotation, string(pr.UID))
	if err != nil {
		return recoveryWait{}, fmt.Errorf("restart ReBAC authorization webhook: %w", err)
	}
	if changed {
		log.Info().Str("step", "rebac-webhook-availability").Msg("restarted ReBAC authorization webhook after KCP webhook bootstrap")
		return recoveryWait{5 * time.Second, "waiting for ReBAC authorization webhook rollout"}, nil
	}
	ready, err := webhook.ready(ctx, p.client)
	if err != nil {
		return recoveryWait{}, fmt.Errorf("check ReBAC authorization webhook: %w", err)
	}
	if !ready {
		return recoveryWait{5 * time.Second, "waiting for ReBAC authorization webhook to become ready"}, nil
	}
	return recoveryWait{}, nil
}

// closeKCPBootstrapWindow restores normal KCP authorization only after the
// bootstrap consumers are healthy, then refreshes front-proxy derived state.
func (p *PlatformRecoverySubroutine) closeKCPBootstrapWindow(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (recoveryWait, error) {
	ready, err := p.ensureKCPWebhookBootstrapRestored(ctx, pr)
	if err != nil {
		return recoveryWait{}, err
	}
	if !ready {
		return recoveryWait{5 * time.Second, "waiting for KCP webhook bootstrap restore"}, nil
	}
	ready, err = p.ensureKCPAuthorizationConfiguration(ctx)
	if err != nil {
		return recoveryWait{}, fmt.Errorf("restore KCP authorization configuration: %w", err)
	}
	if !ready {
		return recoveryWait{10 * time.Second, "waiting for kcp-operator to restore KCP authorization configuration"}, nil
	}
	ready, err = p.ensureKCPFrontProxyReady(ctx, pr)
	if err != nil {
		return recoveryWait{}, fmt.Errorf("restart KCP front-proxy: %w", err)
	}
	if !ready {
		return recoveryWait{10 * time.Second, "waiting for KCP front-proxy cache refresh"}, nil
	}
	return recoveryWait{}, nil
}

func (p *PlatformRecoverySubroutine) ensureKCPWebhookBootstrapDisabled(ctx context.Context, pr *v1alpha1.PlatformRestore) (bool, error) {
	return p.ensureKCPWebhookDisabled(ctx, pr, kcpWebhookBootstrapDisabledKey, kcpWebhookBootstrapRestoredKey)
}

// ensureKCPIdentityRepairWebhookDisabled creates an independent, one-time
// bootstrap window for a restore already in identity validation.
func (p *PlatformRecoverySubroutine) ensureKCPIdentityRepairWebhookDisabled(ctx context.Context, pr *v1alpha1.PlatformRestore) (bool, error) {
	return p.ensureKCPWebhookDisabled(ctx, pr, kcpIdentityRepairWebhookDisabledKey, kcpIdentityRepairWebhookRestoredKey)
}

func (p *PlatformRecoverySubroutine) ensureKCPWebhookBootstrapRestored(ctx context.Context, pr *v1alpha1.PlatformRestore) (bool, error) {
	return p.ensureKCPWebhookRestored(ctx, pr, kcpWebhookBootstrapDisabledKey, kcpWebhookBootstrapRestoredKey)
}

func (p *PlatformRecoverySubroutine) ensureKCPIdentityRepairWebhookRestored(ctx context.Context, pr *v1alpha1.PlatformRestore) (bool, error) {
	return p.ensureKCPWebhookRestored(ctx, pr, kcpIdentityRepairWebhookDisabledKey, kcpIdentityRepairWebhookRestoredKey)
}
