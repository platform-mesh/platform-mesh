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

package velero

import (
	"context"
	"fmt"

	"go.platform-mesh.io/golang-commons/logger"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	veleroServiceAccount = "velero"
	veleroClusterRole    = "velero"
)

// LifecycleRunnable ensures the Velero server Deployment, node-agent DaemonSet,
// ServiceAccount, ClusterRole, and ClusterRoleBinding are present in the operator
// namespace. It runs once after the manager starts (as a controller-runtime Runnable)
// and is idempotent — safe to re-run on restart.
type LifecycleRunnable struct {
	Client    ctrlruntimeclient.Client
	Namespace string
	// Image is the Velero server/node-agent container image.
	// Defaults to config.DefaultVeleroImage; override for air-gapped deployments.
	Image string
}

func (r *LifecycleRunnable) image() string {
	if r.Image != "" {
		return r.Image
	}
	return "velero/velero:v1.18.2"
}

func (r *LifecycleRunnable) Start(ctx context.Context) error {
	log := logger.LoadLoggerFromContext(ctx)

	if err := r.ensureServiceAccount(ctx, log); err != nil {
		log.Warn().Err(err).Msg("unable to ensure Velero ServiceAccount; proceeding")
	}
	if err := r.ensureClusterRole(ctx, log); err != nil {
		log.Warn().Err(err).Msg("unable to ensure Velero ClusterRole; proceeding")
	}
	if err := r.ensureClusterRoleBinding(ctx, log); err != nil {
		log.Warn().Err(err).Msg("unable to ensure Velero ClusterRoleBinding; proceeding")
	}
	if err := r.ensureDeployment(ctx, log); err != nil {
		log.Warn().Err(err).Msg("unable to ensure Velero server Deployment; proceeding")
	}
	if err := r.ensureNodeAgent(ctx, log); err != nil {
		log.Warn().Err(err).Msg("unable to ensure Velero node-agent DaemonSet; proceeding")
	}
	return nil
}

func (r *LifecycleRunnable) ensureServiceAccount(ctx context.Context, log *logger.Logger) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      veleroServiceAccount,
			Namespace: r.Namespace,
		},
	}
	if err := r.Client.Create(ctx, sa); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating Velero ServiceAccount: %w", err)
	}
	log.Info().Str("namespace", r.Namespace).Msg("Velero ServiceAccount present")
	return nil
}

func (r *LifecycleRunnable) ensureClusterRole(ctx context.Context, log *logger.Logger) error {
	desired := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: veleroClusterRole},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"velero.io"}, Resources: []string{"*"}, Verbs: []string{"*"}},
			{APIGroups: []string{""}, Resources: []string{"persistentvolumes", "namespaces", "pods", "persistentvolumeclaims", "serviceaccounts", "secrets", "configmaps", "events", "nodes"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments", "daemonsets", "replicasets", "statefulsets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"storage.k8s.io"}, Resources: []string{"storageclasses"}, Verbs: []string{"get", "list", "watch"}},
		},
	}
	var existing rbacv1.ClusterRole
	if err := r.Client.Get(ctx, types.NamespacedName{Name: veleroClusterRole}, &existing); apierrors.IsNotFound(err) {
		if createErr := r.Client.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("creating Velero ClusterRole: %w", createErr)
		}
		log.Info().Msg("created Velero ClusterRole")
	}
	return nil
}

func (r *LifecycleRunnable) ensureClusterRoleBinding(ctx context.Context, log *logger.Logger) error {
	desired := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "velero"},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      veleroServiceAccount,
			Namespace: r.Namespace,
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     veleroClusterRole,
		},
	}
	var existing rbacv1.ClusterRoleBinding
	if err := r.Client.Get(ctx, types.NamespacedName{Name: "velero"}, &existing); apierrors.IsNotFound(err) {
		if createErr := r.Client.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("creating Velero ClusterRoleBinding: %w", createErr)
		}
		log.Info().Msg("created Velero ClusterRoleBinding")
	}
	return nil
}

func (r *LifecycleRunnable) ensureDeployment(ctx context.Context, log *logger.Logger) error {
	desired := r.veleroDeployment()

	var existing appsv1.Deployment
	err := r.Client.Get(ctx, types.NamespacedName{Name: "velero", Namespace: r.Namespace}, &existing)
	if apierrors.IsNotFound(err) {
		if createErr := r.Client.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("creating Velero Deployment: %w", createErr)
		}
		log.Info().Str("namespace", r.Namespace).Msg("created Velero server Deployment")
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting Velero Deployment: %w", err)
	}

	// Only patch when the image changes to avoid spurious rollouts on every restart.
	if len(existing.Spec.Template.Spec.Containers) > 0 &&
		existing.Spec.Template.Spec.Containers[0].Image == r.image() {
		return nil
	}

	patch := ctrlruntimeclient.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	if patchErr := r.Client.Patch(ctx, &existing, patch); patchErr != nil {
		return fmt.Errorf("updating Velero Deployment: %w", patchErr)
	}
	log.Info().Str("namespace", r.Namespace).Msg("updated Velero server Deployment image")
	return nil
}

func (r *LifecycleRunnable) ensureNodeAgent(ctx context.Context, log *logger.Logger) error {
	desired := r.nodeAgentDaemonSet()

	var existing appsv1.DaemonSet
	err := r.Client.Get(ctx, types.NamespacedName{Name: "node-agent", Namespace: r.Namespace}, &existing)
	if apierrors.IsNotFound(err) {
		if createErr := r.Client.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("creating Velero node-agent DaemonSet: %w", createErr)
		}
		log.Info().Str("namespace", r.Namespace).Msg("created Velero node-agent DaemonSet")
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting Velero node-agent DaemonSet: %w", err)
	}

	// Only patch when the image changes.
	if len(existing.Spec.Template.Spec.Containers) > 0 &&
		existing.Spec.Template.Spec.Containers[0].Image == r.image() {
		return nil
	}

	patch := ctrlruntimeclient.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	if patchErr := r.Client.Patch(ctx, &existing, patch); patchErr != nil {
		return fmt.Errorf("updating Velero node-agent DaemonSet: %w", patchErr)
	}
	log.Info().Str("namespace", r.Namespace).Msg("updated Velero node-agent DaemonSet image")
	return nil
}

func (r *LifecycleRunnable) veleroDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "velero",
			Namespace: r.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "velero"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"app": "velero"},
					Annotations: map[string]string{"prometheus.io/scrape": "true", "prometheus.io/port": "8085"},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: veleroServiceAccount,
					RestartPolicy:      corev1.RestartPolicyAlways,
					Containers: []corev1.Container{
						{
							Name:            "velero",
							Image:           r.image(),
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command: []string{
								"/velero",
								"server",
								"--features=",
								"--uploader-type=kopia",
							},
							Ports: []corev1.ContainerPort{
								{Name: "metrics", ContainerPort: 8085, Protocol: corev1.ProtocolTCP},
							},
							Env: []corev1.EnvVar{
								{Name: "VELERO_SCRATCH_DIR", Value: "/scratch"},
								{Name: "VELERO_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
								}},
								{Name: "LD_LIBRARY_PATH", Value: "/plugins"},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "plugins", MountPath: "/plugins"},
								{Name: "scratch", MountPath: "/scratch"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{Name: "plugins", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: "scratch", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
		},
	}
}

func (r *LifecycleRunnable) nodeAgentDaemonSet() *appsv1.DaemonSet {
	hostPathType := corev1.HostPathDirectoryOrCreate
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node-agent",
			Namespace: r.Namespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"name": "node-agent"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"name": "node-agent"}},
				Spec: corev1.PodSpec{
					ServiceAccountName: veleroServiceAccount,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser: ptr.To(int64(0)),
					},
					Containers: []corev1.Container{
						{
							Name:            "node-agent",
							Image:           r.image(),
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/velero", "node-agent", "server"},
							Env: []corev1.EnvVar{
								{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
								}},
								{Name: "VELERO_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
								}},
								{Name: "VELERO_SCRATCH_DIR", Value: "/scratch"},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "host-pods", MountPath: "/host_pods", MountPropagation: mountPropagationHostToContainer()},
								{Name: "scratch", MountPath: "/scratch"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{Name: "host-pods", VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/kubelet/pods", Type: &hostPathType},
						}},
						{Name: "scratch", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
		},
	}
}

func mountPropagationHostToContainer() *corev1.MountPropagationMode {
	m := corev1.MountPropagationHostToContainer
	return &m
}
