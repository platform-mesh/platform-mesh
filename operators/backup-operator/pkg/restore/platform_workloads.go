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

package restore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	platformMeshNamespace = "platform-mesh-system"
	kcpOperatorNamespace  = "kcp-operator"

	conditionPlatformQuiesced                   = "PlatformQuiesced"
	conditionCredentialsRestored                = "CredentialsRestored"
	conditionOpenFGADataRestored                = "OpenFGADataRestored"
	conditionControlPlaneRestarted              = "ControlPlaneRestarted"
	conditionKCPVirtualWorkspaceClaimsRecovered = "KCPVirtualWorkspaceClaimsRecovered"
	conditionPlatformRecovered                  = "PlatformRecovered"
	conditionIdentityPlaneValidated             = "IdentityPlaneValidated"
	conditionReady                              = "Ready"

	restoreStateConfigMapSuffix       = "platform-restore-state"
	quiescePlatformSubroutineName     = "quiesce-platform"
	controlPlaneRestartSubroutineName = "control-plane-restart"
)

func restoreTerminal(platformRestore *pmbackupv1alpha1.PlatformRestore) bool {
	return platformRestore.Status.Phase == pmbackupv1alpha1.RestorePhaseSucceeded ||
		platformRestore.Status.Phase == pmbackupv1alpha1.RestorePhaseFailed
}

type workloadRef struct {
	Namespace string
	Kind      string
	Name      string
}

type QuiescePlatformSubroutine struct {
	name   string
	client ctrlruntimeclient.Client
}

func NewQuiescePlatformSubroutine(cli ctrlruntimeclient.Client) *QuiescePlatformSubroutine {
	return &QuiescePlatformSubroutine{name: quiescePlatformSubroutineName, client: cli}
}

func (q *QuiescePlatformSubroutine) GetName() string { return q.name }

type ControlPlaneRestartSubroutine struct {
	name   string
	client ctrlruntimeclient.Client
}

func NewControlPlaneRestartSubroutine(cli ctrlruntimeclient.Client) *ControlPlaneRestartSubroutine {
	return &ControlPlaneRestartSubroutine{name: controlPlaneRestartSubroutineName, client: cli}
}

func (c *ControlPlaneRestartSubroutine) GetName() string { return c.name }

func platformDeployment(name string) workloadRef {
	return workloadRef{Namespace: platformMeshNamespace, Kind: "Deployment", Name: name}
}

func platformStatefulSet(name string) workloadRef {
	return workloadRef{Namespace: platformMeshNamespace, Kind: "StatefulSet", Name: name}
}

var controlPlaneRecoveryPrerequisites = []workloadRef{
	platformStatefulSet("openfga-postgres"),
	platformDeployment("openfga"),

	platformDeployment("root-kcp"),
	platformDeployment("nereus-shard-kcp"),
	platformDeployment("triton-shard-kcp"),
	// etcd-cache is recreated empty during restore. Wait for its consumer to
	// come up before allowing proxy and virtual-workspace recovery to proceed.
	platformDeployment("cache-server-cache-server"),
	platformDeployment("root-proxy"),
	platformDeployment("frontproxy-front-proxy"),
}

var restoreManagedWorkloads = []workloadRef{
	platformDeployment("portal"),
	platformDeployment("iam-service"),
	platformDeployment("security-operator"),
	platformDeployment("security-operator-system"),
	platformDeployment("account-operator"),
	platformDeployment("rebac-authz-webhook"),
	platformDeployment("kubernetes-graphql-gateway"),
	platformDeployment("kubernetes-graphql-gateway-listener"),
	platformDeployment("virtual-workspaces"),
	platformDeployment("cache-server-cache-server"),
	platformDeployment("extension-manager-operator-operator"),
	platformDeployment("extension-manager-operator-server"),
	platformDeployment("openfga"),

	platformStatefulSet("keycloak"),
	platformStatefulSet("openfga-postgres"),

	platformDeployment("root-kcp"),
	platformDeployment("nereus-shard-kcp"),
	platformDeployment("triton-shard-kcp"),
	platformDeployment("root-proxy"),
	platformDeployment("frontproxy-front-proxy"),

	{Namespace: kcpOperatorNamespace, Kind: "Deployment", Name: "kcp-operator"},
	platformDeployment("keycloak-operator"),
}

var kcpWebhookConsumers = []workloadRef{
	platformDeployment("root-kcp"),
	platformDeployment("nereus-shard-kcp"),
	platformDeployment("triton-shard-kcp"),
}

func restoreStateConfigMapName(pr *pmbackupv1alpha1.PlatformRestore) string {
	return fmt.Sprintf("%s-%s", pr.Name, restoreStateConfigMapSuffix)
}

func conditionIsTrue(pr *pmbackupv1alpha1.PlatformRestore, conditionType string) bool {
	cond := meta.FindStatusCondition(pr.Status.Conditions, conditionType)
	return cond != nil && cond.Status == metav1.ConditionTrue
}

func setPhase(platformRestore *pmbackupv1alpha1.PlatformRestore, phase pmbackupv1alpha1.RestorePhase) bool {
	changed := false
	if platformRestore.Status.Phase != phase {
		platformRestore.Status.Phase = phase
		changed = true
	}
	if platformRestore.Status.ObservedGeneration != platformRestore.Generation {
		platformRestore.Status.ObservedGeneration = platformRestore.Generation
		changed = true
	}

	return setCondition(
		platformRestore,
		conditionReady,
		metav1.ConditionFalse,
		"RestoreInProgress",
		fmt.Sprintf("Platform restore is in phase %s", phase),
	) || changed
}

func markCondition(pr *pmbackupv1alpha1.PlatformRestore, conditionType, reason, message string) bool {
	return setCondition(pr, conditionType, metav1.ConditionTrue, reason, message)
}

func markPhaseReady(pr *pmbackupv1alpha1.PlatformRestore, conditionType, reason, message string) bool {
	changed := markCondition(pr, conditionType, reason, message)
	if setCondition(
		pr,
		conditionReady,
		metav1.ConditionTrue,
		string(pr.Status.Phase)+"Completed",
		fmt.Sprintf("Platform restore phase %s completed", pr.Status.Phase),
	) {
		changed = true
	}
	return changed
}

func setCondition(
	pr *pmbackupv1alpha1.PlatformRestore,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
	message string,
) bool {
	current := meta.FindStatusCondition(pr.Status.Conditions, conditionType)
	if current != nil &&
		current.Status == status &&
		current.Reason == reason &&
		current.Message == message &&
		current.ObservedGeneration == pr.Generation {
		return false
	}

	meta.SetStatusCondition(&pr.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: pr.Generation,
	})
	return true
}

func markRestoreSucceeded(pr *pmbackupv1alpha1.PlatformRestore) bool {
	changed := false
	if pr.Status.Phase != pmbackupv1alpha1.RestorePhaseSucceeded {
		pr.Status.Phase = pmbackupv1alpha1.RestorePhaseSucceeded
		changed = true
	}
	if pr.Status.ObservedGeneration != pr.Generation {
		pr.Status.ObservedGeneration = pr.Generation
		changed = true
	}
	if markCondition(
		pr,
		conditionIdentityPlaneValidated,
		"IdentityPlaneValidated",
		"kcp child workspace discovery and identity workloads are healthy",
	) {
		changed = true
	}
	if setCondition(
		pr,
		conditionReady,
		metav1.ConditionTrue,
		"PlatformRestoreCompleted",
		"Platform restore completed and identity plane validated",
	) {
		changed = true
	}
	return changed
}

func ensureRestoreStateConfigMap(ctx context.Context, cl ctrlruntimeclient.Client, pr *pmbackupv1alpha1.PlatformRestore) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	key := ctrlruntimeclient.ObjectKey{
		Namespace: platformMeshNamespace,
		Name:      restoreStateConfigMapName(pr),
	}

	err := cl.Get(ctx, key, cm)
	if err == nil {
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		if cm.Data["platformRestoreUID"] == string(pr.UID) {
			return cm, nil
		}

		// A PlatformRestore is commonly recreated with the same name after a
		// failed attempt. Its recovery state is valid only for the restore UID
		// that wrote it; retaining it can skip credential and KCP repairs on the
		// replacement restore.
		if deleteErr := cl.Delete(ctx, cm); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			return nil, fmt.Errorf("delete stale restore state %s/%s: %w", cm.Namespace, cm.Name, deleteErr)
		}
		return nil, fmt.Errorf("deleted stale restore state %s/%s; retry restore reconciliation", cm.Namespace, cm.Name)
	}

	if !apierrors.IsNotFound(err) {
		return nil, err
	}

	cm = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
			Labels: map[string]string{
				"backup.platform-mesh.io/platformrestore": pr.Name,
			},
		},
		Data: map[string]string{
			"platformRestoreUID": string(pr.UID),
		},
	}

	if err := cl.Create(ctx, cm); err != nil {
		return nil, err
	}

	return cm, nil
}

func replicaStateKey(w workloadRef) string {
	return fmt.Sprintf("%s.%s.%s", strings.ToLower(w.Kind), w.Namespace, w.Name)
}

func (w workloadRef) object() (ctrlruntimeclient.Object, error) {
	switch w.Kind {
	case "Deployment":
		return &appsv1.Deployment{}, nil
	case "StatefulSet":
		return &appsv1.StatefulSet{}, nil
	default:
		return nil, fmt.Errorf("unsupported workload kind %q", w.Kind)
	}
}

func workloadReplicas(object ctrlruntimeclient.Object) (int32, error) {
	switch workload := object.(type) {
	case *appsv1.Deployment:
		if workload.Spec.Replicas != nil {
			return *workload.Spec.Replicas, nil
		}
	case *appsv1.StatefulSet:
		if workload.Spec.Replicas != nil {
			return *workload.Spec.Replicas, nil
		}
	default:
		return 0, fmt.Errorf("unsupported workload object %T", object)
	}
	return 1, nil
}

func setWorkloadReplicas(object ctrlruntimeclient.Object, replicas int32) error {
	switch workload := object.(type) {
	case *appsv1.Deployment:
		workload.Spec.Replicas = int32Ptr(replicas)
	case *appsv1.StatefulSet:
		workload.Spec.Replicas = int32Ptr(replicas)
	default:
		return fmt.Errorf("unsupported workload object %T", object)
	}
	return nil
}

func (w workloadRef) rememberReplicas(ctx context.Context, cl ctrlruntimeclient.Client, cm *corev1.ConfigMap) error {
	key := replicaStateKey(w)
	if _, ok := cm.Data[key]; ok {
		return nil
	}

	object, err := w.object()
	if err != nil {
		return err
	}
	err = cl.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: w.Namespace, Name: w.Name}, object)
	if apierrors.IsNotFound(err) {
		cm.Data[key] = "missing"
		return nil
	}
	if err != nil {
		return err
	}

	replicas, err := workloadReplicas(object)
	if err != nil {
		return err
	}
	cm.Data[key] = strconv.Itoa(int(replicas))
	return nil
}

func rememberedReplicas(cm *corev1.ConfigMap, w workloadRef) (int32, bool) {
	raw := cm.Data[replicaStateKey(w)]
	if raw == "" || raw == "missing" {
		return 0, false
	}

	i, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}

	return int32(i), true
}

func int32Ptr(v int32) *int32 {
	return &v
}

func (w workloadRef) scale(ctx context.Context, cl ctrlruntimeclient.Client, replicas int32) (bool, error) {
	object, err := w.object()
	if err != nil {
		return false, err
	}
	err = cl.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: w.Namespace, Name: w.Name}, object)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	current, err := workloadReplicas(object)
	if err != nil {
		return false, err
	}
	if current == replicas {
		return false, nil
	}
	patchBase := object.DeepCopyObject().(ctrlruntimeclient.Object)
	if err := setWorkloadReplicas(object, replicas); err != nil {
		return false, err
	}
	return true, cl.Patch(ctx, object, ctrlruntimeclient.MergeFrom(patchBase))
}

func (w workloadRef) ready(ctx context.Context, cl ctrlruntimeclient.Client) (bool, error) {
	object, err := w.object()
	if err != nil {
		return false, err
	}
	err = cl.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: w.Namespace, Name: w.Name}, object)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	want, err := workloadReplicas(object)
	if err != nil {
		return false, err
	}
	switch workload := object.(type) {
	case *appsv1.Deployment:
		if want == 0 {
			return workload.Status.Replicas == 0, nil
		}
		return workload.Status.ObservedGeneration >= workload.Generation &&
			workload.Status.Replicas == want &&
			workload.Status.UpdatedReplicas >= want &&
			workload.Status.ReadyReplicas >= want &&
			workload.Status.AvailableReplicas >= want &&
			workload.Status.UnavailableReplicas == 0, nil
	case *appsv1.StatefulSet:
		if want == 0 {
			return workload.Status.Replicas == 0, nil
		}
		return workload.Status.ObservedGeneration >= workload.Generation &&
			workload.Status.UpdatedReplicas >= want &&
			workload.Status.ReadyReplicas >= want, nil
	default:
		return false, fmt.Errorf("unsupported workload object %T", object)
	}
}

func restartDeployment(ctx context.Context, cl ctrlruntimeclient.Client, w workloadRef, annotationKey, restoreUID string) (bool, error) {
	var deploy appsv1.Deployment
	err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: w.Namespace, Name: w.Name}, &deploy)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if deploy.Spec.Template.Annotations != nil &&
		deploy.Spec.Template.Annotations[annotationKey] == restoreUID {
		return false, nil
	}

	patchBase := deploy.DeepCopy()
	if deploy.Spec.Template.Annotations == nil {
		deploy.Spec.Template.Annotations = map[string]string{}
	}
	deploy.Spec.Template.Annotations[annotationKey] = restoreUID

	return true, cl.Patch(ctx, &deploy, ctrlruntimeclient.MergeFrom(patchBase))
}

func (q *QuiescePlatformSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	pr, ok := obj.(*pmbackupv1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected pmbackupv1alpha1.PlatformRestore, got %T", obj)
	}
	if restoreTerminal(pr) || conditionIsTrue(pr, conditionPlatformQuiesced) {
		return subroutines.OK(), false, nil
	}
	if changed := setPhase(pr, pmbackupv1alpha1.RestorePhaseQuiescingPlatform); changed {
		return subroutines.StopWithRequeue(time.Second, "phase set to QuiescingPlatform"), true, nil
	}

	log := logger.LoadLoggerFromContext(ctx)
	cm, err := ensureRestoreStateConfigMap(ctx, q.client, pr)
	if err != nil {
		return subroutines.OK(), false, err
	}
	cmPatchBase := cm.DeepCopy()
	for _, w := range restoreManagedWorkloads {
		if err := w.rememberReplicas(ctx, q.client, cm); err != nil {
			return subroutines.OK(), false, err
		}
	}
	if err := q.client.Patch(ctx, cm, ctrlruntimeclient.MergeFrom(cmPatchBase)); err != nil {
		return subroutines.OK(), false, err
	}

	for _, w := range restoreManagedWorkloads {
		changed, err := w.scale(ctx, q.client, 0)
		if err != nil {
			return subroutines.OK(), false, err
		}
		if changed {
			log.Info().Str("subroutine", q.name).Str("platformRestore", pr.Name).Str("namespace", w.Namespace).Str("kind", w.Kind).Str("workload", w.Name).Msg("scaled workload to zero")
		}
	}

	for _, w := range restoreManagedWorkloads {
		ready, err := w.ready(ctx, q.client)
		if err != nil {
			return subroutines.OK(), false, err
		}
		if !ready {
			return subroutines.StopWithRequeue(5*time.Second, fmt.Sprintf("waiting for %s/%s/%s to scale down", w.Namespace, w.Kind, w.Name)), false, nil
		}
	}

	return subroutines.OK(), markPhaseReady(pr, conditionPlatformQuiesced, "PlatformQuiesced", "Platform control-plane and identity workloads were scaled down before restore"), nil
}

func (c *ControlPlaneRestartSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	pr, ok := obj.(*pmbackupv1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected pmbackupv1alpha1.PlatformRestore, got %T", obj)
	}
	if restoreTerminal(pr) || conditionIsTrue(pr, conditionControlPlaneRestarted) {
		return subroutines.OK(), false, nil
	}
	if changed := setPhase(pr, pmbackupv1alpha1.RestorePhaseRestartingControlPlane); changed {
		return subroutines.StopWithRequeue(time.Second, "phase set to RestartingControlPlane"), true, nil
	}

	log := logger.LoadLoggerFromContext(ctx)
	cm, err := ensureRestoreStateConfigMap(ctx, c.client, pr)
	if err != nil {
		return subroutines.OK(), false, err
	}
	for _, w := range restoreManagedWorkloads {
		replicas, ok := rememberedReplicas(cm, w)
		if !ok {
			continue
		}
		if _, err := w.scale(ctx, c.client, replicas); err != nil && !apierrors.IsNotFound(err) {
			return subroutines.OK(), false, err
		}
	}

	for _, w := range controlPlaneRecoveryPrerequisites {
		ready, err := w.ready(ctx, c.client)
		if err != nil {
			return subroutines.OK(), false, err
		}
		if !ready {
			log.Info().Str("subroutine", c.name).Str("platformRestore", pr.Name).Str("namespace", w.Namespace).Str("kind", w.Kind).Str("workload", w.Name).Msg("waiting for recovery prerequisite workload")
			return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("waiting for recovery prerequisite %s/%s/%s", w.Namespace, w.Kind, w.Name)), false, nil
		}
	}

	return subroutines.OK(), markCondition(pr, conditionControlPlaneRestarted, "ControlPlaneRestarted", "core control-plane workloads are ready for post-restore recovery"), nil
}
