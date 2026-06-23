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
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/apis/terminal/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	PodSubroutineName      = "PodSubroutine"
	PodSubroutineFinalizer = "terminal.platform-mesh.io/pod-finalizer"
	DefaultAccountInfoName = "account"
	PodRequeueAfter        = 5 * time.Second
)

// PodSubroutine manages terminal pods on the runtime cluster
type PodSubroutine struct {
	mgr            mcmanager.Manager
	runtimeClient  client.Client
	terminalImage  string
	namespace      string
	hostAliasIP    string
	hostAliasNames []string
}

func NewPodSubroutine(mgr mcmanager.Manager, runtimeClient client.Client, terminalImage, namespace, hostAliasIP string, hostAliasNames []string) *PodSubroutine {
	return &PodSubroutine{
		mgr:            mgr,
		runtimeClient:  runtimeClient,
		terminalImage:  terminalImage,
		namespace:      namespace,
		hostAliasIP:    hostAliasIP,
		hostAliasNames: hostAliasNames,
	}
}

func (r *PodSubroutine) GetName() string {
	return PodSubroutineName
}

func (r *PodSubroutine) Finalizers(_ client.Object) []string { // coverage-ignore
	return []string{PodSubroutineFinalizer}
}

func (r *PodSubroutine) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	instance := obj.(*v1alpha1.Terminal)
	log := logger.LoadLoggerFromContext(ctx)

	if instance.Status.PodName == "" {
		return subroutines.OK(), nil
	}

	pod := &corev1.Pod{}
	podKey := client.ObjectKey{Namespace: r.namespace, Name: instance.Status.PodName}

	if err := r.runtimeClient.Get(ctx, podKey, pod); err != nil {
		if kerrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return subroutines.OK(), nil
		}
		return subroutines.OK(), err
	}

	if pod.GetDeletionTimestamp() != nil {
		log.Debug().Str("podName", pod.Name).Msg("pod is already being deleted, waiting")
		return subroutines.StopWithRequeue(PodRequeueAfter, "pod is already being deleted"), nil
	}

	log.Info().Str("podName", pod.Name).Msg("deleting terminal pod")
	if err := r.runtimeClient.Delete(ctx, pod); err != nil {
		if kerrors.IsNotFound(err) {
			return subroutines.OK(), nil
		}
		return subroutines.OK(), err
	}

	return subroutines.StopWithRequeue(PodRequeueAfter, "terminal pod deletion requested"), nil
}

func (r *PodSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	instance := obj.(*v1alpha1.Terminal)
	log := logger.LoadLoggerFromContext(ctx)

	// Get cluster name from multicluster context
	clusterName, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.OK(), fmt.Errorf("cluster name not found in context")
	}

	// Get the cluster client for this workspace
	cluster, err := r.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return subroutines.OK(), err
	}
	clusterClient := cluster.GetClient()

	// Look up AccountInfo to get workspace URL
	accountInfo := &corev1alpha1.AccountInfo{}
	if err := clusterClient.Get(ctx, client.ObjectKey{Name: DefaultAccountInfoName}, accountInfo); err != nil {
		if kerrors.IsNotFound(err) {
			log.Warn().Msg("AccountInfo not found, waiting for it to be created")
			return subroutines.StopWithRequeue(PodRequeueAfter, "AccountInfo not found yet"), nil
		}
		return subroutines.OK(), err
	}

	workspaceURL := accountInfo.Spec.Account.URL
	workspacePath := accountInfo.Spec.Account.Path
	clusterCA := accountInfo.Spec.ClusterInfo.CA

	if workspaceURL == "" {
		log.Warn().Msg("AccountInfo.spec.account.url is empty, waiting")
		return subroutines.StopWithRequeue(PodRequeueAfter, "AccountInfo.spec.account.url is empty"), nil
	}

	if clusterCA == "" {
		log.Warn().Msg("AccountInfo.spec.clusterInfo.ca is empty, waiting")
		return subroutines.StopWithRequeue(PodRequeueAfter, "AccountInfo.spec.clusterInfo.ca is empty"), nil
	}

	// Update workspace path in status
	instance.Status.WorkspacePath = workspacePath

	// Generate session ID if not set (used for non-guessable URL path)
	if instance.Status.SessionID == "" {
		instance.Status.SessionID = uuid.New().String()
	}

	// Capture creator identity from annotations (set by kcp from OIDC token)
	if instance.Status.CreatedBy == "" {
		// kcp sets user identity in annotations like "kcp.io/user-info"
		if userInfo, ok := instance.Annotations["kcp.io/user-info"]; ok {
			instance.Status.CreatedBy = userInfo
		} else if creator, ok := instance.Annotations["kcp.io/creator"]; ok {
			instance.Status.CreatedBy = creator
		}
	}

	// Generate pod name from terminal name
	podName := fmt.Sprintf("terminal-%s", instance.Name)

	// Check if pod already exists
	existingPod := &corev1.Pod{}
	podKey := client.ObjectKey{Namespace: r.namespace, Name: podName}

	if err := r.runtimeClient.Get(ctx, podKey, existingPod); err != nil {
		if !kerrors.IsNotFound(err) {
			return subroutines.OK(), err
		}

		// Pod doesn't exist, create it
		log.Info().Str("podName", podName).Str("namespace", r.namespace).Str("workspace", workspacePath).Str("url", workspaceURL).Msg("creating terminal pod")

		// Base64 encode CA for kubeconfig certificate-authority-data field
		clusterCAEncoded := base64.StdEncoding.EncodeToString([]byte(clusterCA))
		pod := r.buildTerminalPod(instance, podName, workspaceURL, clusterCAEncoded)
		if err := r.runtimeClient.Create(ctx, pod); err != nil {
			return subroutines.OK(), err
		}

		// Update status
		instance.Status.PodName = podName
		instance.Status.Phase = v1alpha1.TerminalPhaseCreating
		// Requeue to check pod status
		return subroutines.StopWithRequeue(PodRequeueAfter, "terminal pod created, waiting for readiness"), nil
	}

	// Pod exists, update status based on pod phase
	instance.Status.PodName = podName
	switch existingPod.Status.Phase {
	case corev1.PodRunning:
		instance.Status.Phase = v1alpha1.TerminalPhaseReady
		return subroutines.OK(), nil
	case corev1.PodFailed:
		instance.Status.Phase = v1alpha1.TerminalPhaseFailed
		return subroutines.OK(), nil
	case corev1.PodPending:
		instance.Status.Phase = v1alpha1.TerminalPhaseCreating
	default:
		instance.Status.Phase = v1alpha1.TerminalPhasePending
	}

	// Pod not yet ready, requeue to check again
	return subroutines.StopWithRequeue(PodRequeueAfter, "terminal pod is not ready yet"), nil
}

func (r *PodSubroutine) buildTerminalPod(terminal *v1alpha1.Terminal, podName, workspaceURL, clusterCA string) *corev1.Pod {
	envVars := []corev1.EnvVar{
		{
			Name:  "KCP_WORKSPACE_URL",
			Value: workspaceURL,
		},
		{
			Name:  "KCP_CA_DATA",
			Value: clusterCA,
		},
	}

	// Add expected user ID for authentication if available
	if terminal.Status.CreatedBy != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "EXPECTED_USER_ID",
			Value: terminal.Status.CreatedBy,
		})
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: r.namespace,
			Labels: map[string]string{
				nameLabel:         nameLabelValue,
				instanceLabel:     terminal.Name,
				managedByLabel:    managedBy,
				terminalNameLabel: terminal.Name,
			},
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: ptr.To(false),
			RestartPolicy:                corev1.RestartPolicyNever,
			Volumes: []corev1.Volume{
				{
					Name: "tmp",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "home",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "terminal",
					Image:           r.terminalImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Env:             envVars,
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 8080,
							Protocol:      corev1.ProtocolTCP,
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "tmp", MountPath: "/tmp"},
						{Name: "home", MountPath: "/home/terminal"},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(false),
						ReadOnlyRootFilesystem:   ptr.To(true),
						RunAsNonRoot:             ptr.To(true),
						RunAsUser:                ptr.To(int64(1000)),
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
					},
				},
			},
		},
	}

	// Add hostAliases if configured (for local development)
	if r.hostAliasIP != "" && len(r.hostAliasNames) > 0 {
		pod.Spec.HostAliases = []corev1.HostAlias{
			{
				IP:        r.hostAliasIP,
				Hostnames: r.hostAliasNames,
			},
		}
	}

	return pod
}

var _ subroutines.Processor = (*PodSubroutine)(nil)
var _ subroutines.Finalizer = (*PodSubroutine)(nil)
