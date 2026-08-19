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
	"time"

	velerov1crds "github.com/vmware-tanzu/velero/config/crd/v1/crds"
	velerov2alpha1crds "github.com/vmware-tanzu/velero/config/crd/v2alpha1/crds"

	"go.platform-mesh.io/golang-commons/logger"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	DefaultNamespace          = "platform-mesh-velero"
	veleroName                = "velero"
	veleroInstallName         = "velero-install"
	nodeAgentName             = "node-agent"
	veleroImageTag            = "velero/velero:v1.18.0"
	objectStorePluginImageTag = "velero/velero-plugin-for-aws:v1.14.2"
)

type Installer struct {
	client                 ctrlruntimeclient.Client
	name                   string
	namespace              string
	veleroImage            string
	objectStorePluginImage string
}

func NewInstaller(client ctrlruntimeclient.Client) *Installer {
	return &Installer{
		name:   veleroInstallName,
		client: client,
	}
}

func (i *Installer) Ensure(ctx context.Context) error {
	if i.client == nil {
		return fmt.Errorf("client is nil")
	}

	if i.namespace == "" {
		i.namespace = DefaultNamespace
	}

	if i.veleroImage == "" {
		i.veleroImage = veleroImageTag
	}

	if i.objectStorePluginImage == "" {
		i.objectStorePluginImage = objectStorePluginImageTag
	}

	log := logger.LoadLoggerFromContext(ctx)

	log.Info().
		Str("subroutine", "velero-install").
		Str("namespace", i.namespace).
		Str("veleroImage", i.veleroImage).
		Str("objectStorePluginImage", i.objectStorePluginImage).
		Msg("ensuring Velero installation")

	if err := i.ensureCRDs(ctx); err != nil {
		log.Error().Err(err).Str("subroutine", "velero-install").Msg("failed to ensure Velero CRDs")
		return err
	}

	if err := i.waitForCRDs(ctx); err != nil {
		log.Error().Err(err).Str("subroutine", veleroInstallName).Msg("Velero CRDs did not become ready")
		return err
	}

	if err := i.ensureNamespace(ctx); err != nil {
		log.Error().Err(err).Str("subroutine", veleroInstallName).Msg("failed to ensure Velero namespace")
		return err
	}

	if err := i.ensureServiceAccount(ctx); err != nil {
		log.Error().Err(err).Str("subroutine", veleroInstallName).Msg("failed to ensure Velero service account")
		return err
	}

	if err := i.ensureServerDeployment(ctx); err != nil {
		log.Error().Err(err).Str("subroutine", veleroInstallName).Msg("failed to ensure Velero server deployment")
		return err
	}

	if err := i.ensureNodeAgent(ctx); err != nil {
		log.Error().Err(err).Str("subroutine", veleroInstallName).Msg("failed to ensure Velero node-agent")
		return err
	}

	log.Info().
		Str("subroutine", veleroInstallName).
		Str("namespace", i.namespace).
		Msg("Velero installation reconciled")

	return nil
}

func (i *Installer) ensureCRDs(ctx context.Context) error {
	log := logger.LoadLoggerFromContext(ctx)

	for _, crd := range allCRDs() {
		desired := crd.DeepCopy()
		desired.Labels = mergeLabels(desired.Labels)

		log.Debug().
			Str("subroutine", veleroInstallName).
			Str("crd", desired.Name).
			Msg("ensuring Velero CRD")

		if err := i.applyCRD(ctx, desired); err != nil {
			return fmt.Errorf("failed to apply Velero CRD %s: %w", desired.Name, err)
		}
	}

	return nil
}

func (i *Installer) applyCRD(ctx context.Context, desired *apiextensionsv1.CustomResourceDefinition) error {
	log := logger.LoadLoggerFromContext(ctx)

	current := &apiextensionsv1.CustomResourceDefinition{}

	err := i.client.Get(ctx, types.NamespacedName{Name: desired.Name}, current)
	if apierrors.IsNotFound(err) {
		log.Info().
			Str("subroutine", veleroInstallName).
			Str("resource", "crd").
			Str("name", desired.Name).
			Msg("creating Velero CRD")

		return i.client.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	current.Labels = mergeLabels(current.Labels)
	current.Annotations = desired.Annotations
	current.Spec = desired.Spec

	log.Debug().
		Str("subroutine", veleroInstallName).
		Str("resource", "crd").
		Str("name", desired.Name).
		Msg("updating Velero CRD")

	return i.client.Update(ctx, current)
}

func (i *Installer) waitForCRDs(ctx context.Context) error {
	log := logger.LoadLoggerFromContext(ctx)

	log.Debug().
		Str("subroutine", veleroInstallName).
		Msg("waiting for Velero CRDs to become established")

	err := wait.PollUntilContextTimeout(
		ctx,
		500*time.Millisecond,
		45*time.Second,
		true,
		func(ctx context.Context) (bool, error) {
			return i.veleroCRDsEstablished(ctx)
		},
	)
	if err != nil {
		return fmt.Errorf("wait for Velero CRDs to become Established: %w", err)
	}

	log.Info().
		Str("subroutine", veleroInstallName).
		Msg("Velero CRDs are established")

	return nil
}

func (i *Installer) veleroCRDsEstablished(ctx context.Context) (bool, error) {
	for _, crd := range allCRDs() {
		current := &apiextensionsv1.CustomResourceDefinition{}

		err := i.client.Get(ctx, types.NamespacedName{Name: crd.Name}, current)
		if err != nil {
			return false, err
		}

		if !isEstablished(current) {
			return false, nil
		}
	}

	return true, nil
}

func isEstablished(crd *apiextensionsv1.CustomResourceDefinition) bool {
	for _, condition := range crd.Status.Conditions {
		if condition.Type == apiextensionsv1.Established &&
			condition.Status == apiextensionsv1.ConditionTrue {
			return true
		}
	}

	return false
}

func (i *Installer) ensureNamespace(ctx context.Context) error {
	log := logger.LoadLoggerFromContext(ctx)

	log.Debug().
		Str("subroutine", veleroInstallName).
		Str("resource", "namespace").
		Str("name", i.namespace).
		Msg("ensuring Velero namespace")

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: i.namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, i.client, ns, func() error {
		ns.Labels = objectLabels()
		return nil
	})

	if err != nil {
		return fmt.Errorf("ensure Velero namespace: %w", err)
	}

	return nil
}

func (i *Installer) ensureServiceAccount(ctx context.Context) error {
	log := logger.LoadLoggerFromContext(ctx)

	log.Debug().
		Str("subroutine", veleroInstallName).
		Str("resource", "serviceaccount").
		Str("name", veleroName).
		Msg("ensuring Velero service account")

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      veleroName,
			Namespace: i.namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, i.client, sa, func() error {
		sa.Labels = objectLabels()
		return nil
	})

	if err != nil {
		return fmt.Errorf("ensure Velero service account: %w", err)
	}

	return nil
}

func (i *Installer) ensureServerDeployment(ctx context.Context) error {
	log := logger.LoadLoggerFromContext(ctx)

	log.Debug().
		Str("subroutine", veleroInstallName).
		Str("resource", "deployment").
		Str("name", veleroName).
		Msg("ensuring Velero server deployment")

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      veleroName,
			Namespace: i.namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, i.client, deploy, func() error {
		deploy.Labels = objectLabels()
		deploy.Spec.Replicas = ptr.To[int32](1)
		serverLabels := podLabels("server")
		deploy.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: serverLabels,
		}
		deploy.Spec.Template.Labels = serverLabels
		deploy.Spec.Template.Spec.ServiceAccountName = veleroName
		deploy.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyAlways

		deploy.Spec.Template.Spec.InitContainers = []corev1.Container{
			{
				Name:  "velero-plugin-for-aws",
				Image: i.objectStorePluginImage,
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "plugins",
						MountPath: "/target",
					},
				},
			},
		}

		deploy.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:    veleroName,
				Image:   i.veleroImage,
				Command: []string{"/velero"},
				Args:    []string{"server"},
				Ports: []corev1.ContainerPort{
					{
						Name:          "metrics",
						ContainerPort: 8085,
					},
				},
				Env: []corev1.EnvVar{
					namespaceEnv(),
					{
						Name:  "VELERO_SCRATCH_DIR",
						Value: "/scratch",
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "plugins",
						MountPath: "/plugins",
					},
					{
						Name:      "scratch",
						MountPath: "/scratch",
					},
				},
			},
		}

		deploy.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "plugins",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
			{
				Name: "scratch",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to ensure Velero deployment: %w", err)
	}

	return nil
}

func (i *Installer) ensureNodeAgent(ctx context.Context) error {
	log := logger.LoadLoggerFromContext(ctx)

	log.Debug().
		Str("subroutine", veleroInstallName).
		Str("resource", "daemonset").
		Str("name", nodeAgentName).
		Msg("ensuring Velero daemonset")

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeAgentName,
			Namespace: i.namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, i.client, ds, func() error {
		hostToContainer := corev1.MountPropagationHostToContainer
		ds.Labels = objectLabels()
		agentLabels := podLabels("node-agent")
		ds.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: agentLabels,
		}

		ds.Spec.Template.Labels = agentLabels
		ds.Spec.Template.Spec.ServiceAccountName = veleroName
		ds.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyAlways

		ds.Spec.Template.Spec.Tolerations = []corev1.Toleration{
			{
				Operator: corev1.TolerationOpExists,
			},
		}

		ds.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:    nodeAgentName,
				Image:   i.veleroImage,
				Command: []string{"/velero"},
				Args: []string{
					"node-agent",
					"server",
				},
				// The node-agent needs access to kubelet pod volume directories for
				// filesystem backup/restore. In Kind this requires privileged access.
				SecurityContext: &corev1.SecurityContext{
					Privileged: ptr.To(true),
					RunAsUser:  ptr.To[int64](0),
					RunAsGroup: ptr.To[int64](0),
				},
				Ports: []corev1.ContainerPort{
					{
						Name:          "metrics",
						ContainerPort: 8085,
					},
				},
				Env: []corev1.EnvVar{
					namespaceEnv(),
					{
						Name: "NODE_NAME",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "spec.nodeName",
							},
						},
					},
					{
						Name:  "VELERO_SCRATCH_DIR",
						Value: "/scratch",
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:             "host-pods",
						MountPath:        "/host_pods",
						MountPropagation: &hostToContainer,
					},
					{
						Name:             "host-plugins",
						MountPath:        "/host_plugins",
						MountPropagation: &hostToContainer,
					},
					{
						Name:      "scratch",
						MountPath: "/scratch",
					},
				},
			},
		}

		ds.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "host-pods",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/var/lib/kubelet/pods",
					},
				},
			},
			{
				Name: "host-plugins",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/var/lib/kubelet/plugins",
					},
				},
			},
			{
				Name: "scratch",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to ensure Velero node-agent daemonset: %w", err)
	}

	return nil
}

func namespaceEnv() corev1.EnvVar {
	return corev1.EnvVar{
		Name: "VELERO_NAMESPACE",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "metadata.namespace",
			},
		},
	}
}

func objectLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       veleroName,
		"app.kubernetes.io/managed-by": "platform-mesh-backup-operator",
		"app.kubernetes.io/part-of":    "platform-mesh",
	}
}

func podLabels(component string) map[string]string {
	labels := objectLabels()
	labels["app.kubernetes.io/component"] = component
	return labels
}

func mergeLabels(existing map[string]string) map[string]string {
	labels := map[string]string{}

	for key, value := range existing {
		labels[key] = value
	}

	for key, value := range objectLabels() {
		labels[key] = value
	}

	return labels
}

func allCRDs() []*apiextensionsv1.CustomResourceDefinition {
	return append(velerov1crds.CRDs, velerov2alpha1crds.CRDs...)
}
