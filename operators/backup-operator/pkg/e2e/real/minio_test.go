//go:build e2e_real

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

package e2e_real_test

import (
	"bytes"
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	clientgoscheme2 "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	minioDeploymentName = "minio"
	minioServiceName    = "minio"
	minioSecretName     = "s3-credentials"
	minioBucket         = "backups"
	minioUser           = "minioadmin"
	minioPassword       = "minioadmin"
	minioPort           = int32(9000)
)

// EnsureMinioDeployed idempotently deploys minio into ns and creates the
// s3-credentials Secret and the "backups" bucket. Returns nil if minio is
// already running and the bucket already exists.
func EnsureMinioDeployed(ctx context.Context, cl ctrlruntimeclient.Client, ns string) error {
	if err := applyMinioDeployment(ctx, cl, ns); err != nil {
		return fmt.Errorf("minio deployment: %w", err)
	}
	if err := applyMinioService(ctx, cl, ns); err != nil {
		return fmt.Errorf("minio service: %w", err)
	}
	if err := applyMinioSecret(ctx, cl, ns); err != nil {
		return fmt.Errorf("minio secret: %w", err)
	}
	if err := waitMinioReady(ctx, cl, ns); err != nil {
		return fmt.Errorf("waiting for minio ready: %w", err)
	}
	if err := ensureBucket(ctx, cl, ns); err != nil {
		return fmt.Errorf("creating bucket %s: %w", minioBucket, err)
	}
	return nil
}

// EnsureMinioTeardown deletes all minio resources from ns. Errors are collected
// and returned as a single error; missing resources are silently ignored.
func EnsureMinioTeardown(ctx context.Context, cl ctrlruntimeclient.Client, ns string) error {
	del := func(obj ctrlruntimeclient.Object) error {
		err := cl.Delete(ctx, obj)
		return ctrlruntimeclient.IgnoreNotFound(err)
	}

	var errs []error
	if err := del(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: minioDeploymentName, Namespace: ns}}); err != nil {
		errs = append(errs, fmt.Errorf("delete minio deployment: %w", err))
	}
	if err := del(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: minioServiceName, Namespace: ns}}); err != nil {
		errs = append(errs, fmt.Errorf("delete minio service: %w", err))
	}
	if err := del(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: minioSecretName, Namespace: ns}}); err != nil {
		errs = append(errs, fmt.Errorf("delete s3-credentials secret: %w", err))
	}
	// Delete any lingering bucket-creation jobs.
	var jobs batchv1.JobList
	if err := cl.List(ctx, &jobs, ctrlruntimeclient.InNamespace(ns),
		ctrlruntimeclient.MatchingLabels{"app": "minio-init"}); err == nil {
		for i := range jobs.Items {
			j := &jobs.Items[i]
			propagation := metav1.DeletePropagationBackground
			_ = cl.Delete(ctx, j, &ctrlruntimeclient.DeleteOptions{PropagationPolicy: &propagation})
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("minio teardown errors: %v", errs)
	}
	return nil
}

// VerifyS3SnapshotExists verifies that etcdbr wrote at least one snapshot to
// minio by exec-ing into the minio pod and checking the data directory.
// etcdbr stores snapshots at /data/<bucket>/<prefix>/v2/Full-*.gz using
// minio's erasure-coded on-disk layout. Checking the filesystem directly
// avoids mc client network/timing issues.
func VerifyS3SnapshotExists(ctx context.Context, cl ctrlruntimeclient.Client, ns, shardPrefix string) error {
	var pods corev1.PodList
	if err := cl.List(ctx, &pods,
		ctrlruntimeclient.InNamespace(ns),
		ctrlruntimeclient.MatchingLabels{"app": "minio"},
	); err != nil {
		return fmt.Errorf("listing minio pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no minio pod found in namespace %s", ns)
	}
	minioPodName := pods.Items[0].Name

	// etcdbr writes to /data/<bucket>/<prefix>/v2/ and each snapshot is a
	// directory named Full-*.gz containing xl.meta + part data.
	dataPath := fmt.Sprintf("/data/%s/%s/v2", minioBucket, shardPrefix)

	deadline := time.Now().Add(2 * time.Minute)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = execInPod(ctx, ns, minioPodName, "minio",
			[]string{"sh", "-c", "test $(ls " + dataPath + " | wc -l) -gt 0"})
		if lastErr == nil {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("no Full snapshot in minio at %s after 2 minutes: %w", dataPath, lastErr)
}

// ── internal helpers ──────────────────────────────────────────────────────────

func applyMinioDeployment(ctx context.Context, cl ctrlruntimeclient.Client, ns string) error {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: minioDeploymentName, Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "minio"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "minio"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "minio",
							Image: "minio/minio:latest",
							Args:  []string{"server", "/data", "--console-address", ":9001"},
							Env: []corev1.EnvVar{
								{Name: "MINIO_ROOT_USER", Value: minioUser},
								{Name: "MINIO_ROOT_PASSWORD", Value: minioPassword},
							},
							Ports: []corev1.ContainerPort{
								{Name: "api", ContainerPort: minioPort},
								{Name: "console", ContainerPort: 9001},
							},
						},
					},
				},
			},
		},
	}
	err := cl.Create(ctx, deploy)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func applyMinioService(ctx context.Context, cl ctrlruntimeclient.Client, ns string) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: minioServiceName, Namespace: ns},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "minio"},
			Ports: []corev1.ServicePort{
				{Name: "api", Port: minioPort, Protocol: corev1.ProtocolTCP},
			},
		},
	}
	err := cl.Create(ctx, svc)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func applyMinioSecret(ctx context.Context, cl ctrlruntimeclient.Client, ns string) error {
	minioEndpoint := fmt.Sprintf("http://%s.%s.svc:%d", minioServiceName, ns, minioPort)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: minioSecretName, Namespace: ns},
		Data: map[string][]byte{
			"accessKeyID":      []byte(minioUser),
			"secretAccessKey":  []byte(minioPassword),
			"region":           []byte("us-east-1"),
			"endpoint":         []byte(minioEndpoint),
			"s3ForcePathStyle": []byte("true"),
		},
	}
	err := cl.Create(ctx, secret)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func waitMinioReady(ctx context.Context, cl ctrlruntimeclient.Client, ns string) error {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		var deploy appsv1.Deployment
		if err := cl.Get(ctx, types.NamespacedName{Name: minioDeploymentName, Namespace: ns}, &deploy); err == nil {
			if deploy.Status.ReadyReplicas >= 1 {
				return nil
			}
		}
		fmt.Printf("waiting for minio deployment %s/%s to be ready...\n", ns, minioDeploymentName)
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("minio deployment %s/%s not ready after 3 minutes", ns, minioDeploymentName)
}

func ensureBucket(ctx context.Context, cl ctrlruntimeclient.Client, ns string) error {
	jobName := "minio-init-bucket"
	minioEndpoint := fmt.Sprintf("http://%s:%d", minioServiceName, minioPort)
	cmd := fmt.Sprintf(
		"mc alias set local %s %s %s && mc mb --ignore-existing local/%s",
		minioEndpoint, minioUser, minioPassword, minioBucket,
	)

	propagation := metav1.DeletePropagationBackground
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: ns,
			Labels:    map[string]string{"app": "minio-init"},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To(int32(3)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "minio-init"}},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    "mc",
							Image:   "minio/mc:latest",
							Command: []string{"/bin/sh", "-c", cmd},
						},
					},
				},
			},
		},
	}

	err := cl.Create(ctx, job)
	if apierrors.IsAlreadyExists(err) {
		// Already ran from a previous setup — skip waiting.
		return nil
	}
	if err != nil {
		return fmt.Errorf("creating bucket init job: %w", err)
	}

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		var j batchv1.Job
		if err := cl.Get(ctx, types.NamespacedName{Name: jobName, Namespace: ns}, &j); err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		if j.Status.Succeeded >= 1 {
			_ = cl.Delete(context.Background(), job,
				&ctrlruntimeclient.DeleteOptions{PropagationPolicy: &propagation})
			return nil
		}
		if j.Status.Failed > *job.Spec.BackoffLimit {
			return fmt.Errorf("bucket init job exceeded backoff limit")
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("bucket init job timed out")
}

// execInPod runs cmd in the named container of podName and returns an error
// if the command exits non-zero or cannot be started.
func execInPod(ctx context.Context, ns, podName, containerName string, cmd []string) error {
	_, err := execInPodOutput(ctx, ns, podName, containerName, cmd)
	return err
}

// execInPodOutput runs cmd and returns stdout + error.
func execInPodOutput(ctx context.Context, ns, podName, containerName string, cmd []string) (string, error) {
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return "", fmt.Errorf("building clientset: %w", err)
	}
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(ns).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdout:    true,
			Stderr:    true,
		}, clientgoscheme2.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(restCfg, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("creating executor: %w", err)
	}
	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", fmt.Errorf("exec failed (stderr=%q): %w", stderr.String(), err)
	}
	return stdout.String(), nil
}
