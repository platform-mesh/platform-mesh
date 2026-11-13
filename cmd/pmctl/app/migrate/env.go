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

package migrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog/v2"
)

const (
	// profileConfigMap is the installation profile the operator reads, and
	// profileKey the key the profile is under.
	profileConfigMap = "platform-mesh-profile"
	profileKey       = "profile.yaml"
	// platformMesh is the name of the PlatformMesh resource and of the OCM
	// Component of the installation.
	platformMesh = "platform-mesh"

	pollInterval = 3 * time.Second
)

var (
	componentGVR    = schema.GroupVersionResource{Group: "delivery.ocm.software", Version: "v1alpha1", Resource: "components"}
	platformMeshGVR = schema.GroupVersionResource{Group: "core.platform-mesh.io", Version: "v1alpha1", Resource: "platformmeshes"}
	helmReleaseGVR  = schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"}
)

// Env is what a migration works with: the cluster Platform Mesh is installed
// in, and the directory for its backups.
type Env struct {
	// Namespace Platform Mesh is installed in.
	Namespace string
	// BackupDir is where the migration keeps what it saves. The After part
	// of a migration finds there what its Before part left.
	BackupDir string

	out     io.Writer
	config  *rest.Config
	kube    kubernetes.Interface
	dynamic dynamic.Interface
}

func newEnv(o *Options) (*Env, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = o.Kubeconfig
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading the kubeconfig: %w", err)
	}
	kube, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &Env{
		Namespace: o.Namespace,
		BackupDir: o.BackupDir,
		out:       o.ErrOut,
		config:    config,
		kube:      kube,
		dynamic:   dyn,
	}, nil
}

// step runs fn as a named step and reports it like "pmctl quickstart" does.
func (e *Env) step(name string, fn func() error) error {
	start := time.Now()
	err := fn()
	mark := "✓"
	if err != nil {
		mark = "✗"
	}
	_, _ = fmt.Fprintf(e.out, " %s %s (%s)\n", mark, name, time.Since(start).Round(time.Second))
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// notef prints a line for the user.
func (e *Env) notef(format string, args ...any) {
	_, _ = fmt.Fprintf(e.out, "   "+format+"\n", args...)
}

// logf prints a debug line, shown with -v=1 or higher.
func (e *Env) logf(format string, args ...any) {
	if klog.V(1).Enabled() {
		_, _ = fmt.Fprintf(e.out, ">>> "+format+"\n", args...)
	}
}

// poll runs check every few seconds until it reports done, fails, or the
// context ends. The status it returns is part of the error then.
func (e *Env) poll(ctx context.Context, check func(context.Context) (done bool, status string, err error)) error {
	var last string
	err := wait.PollUntilContextCancel(ctx, pollInterval, true, func(ctx context.Context) (bool, error) {
		done, status, err := check(ctx)
		if err != nil || done {
			return done, err
		}
		if status != "" && status != last {
			e.logf("%s", status)
			last = status
		}
		return false, nil
	})
	if err != nil && last != "" {
		return fmt.Errorf("%w (last status: %s)", err, last)
	}
	return err
}

// installedVersion is the version of the OCM component the installation
// runs.
func (e *Env) installedVersion(ctx context.Context) (string, error) {
	component, err := e.dynamic.Resource(componentGVR).Namespace(e.Namespace).Get(ctx, platformMesh, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	version, _, _ := unstructured.NestedString(component.Object, "status", "component", "version")
	if version == "" {
		return "", fmt.Errorf("component %s/%s reports no version yet", e.Namespace, platformMesh)
	}
	return version, nil
}

// helmValue returns the value at path in the values of a HelmRelease, as the
// operator rendered them from the profile. found is false when the release
// or the value is not there.
func (e *Env) helmValue(ctx context.Context, release string, path ...string) (value any, found bool, err error) {
	hr, err := e.dynamic.Resource(helmReleaseGVR).Namespace(e.Namespace).Get(ctx, release, metav1.GetOptions{})
	if err != nil {
		return nil, false, fmt.Errorf("HelmRelease %s: %w", release, err)
	}
	return unstructured.NestedFieldNoCopy(hr.Object, append([]string{"spec", "values"}, path...)...)
}

// setProfile sets values in the installation profile and has the operator
// reconcile it. Every key of set is a path below the root of the profile,
// its segments separated by dots.
func (e *Env) setProfile(ctx context.Context, set map[string]any) error {
	configMaps := e.kube.CoreV1().ConfigMaps(e.Namespace)
	cm, err := configMaps.Get(ctx, profileConfigMap, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("reading the profile: %w", err)
	}
	profile, err := setYAML(cm.Data[profileKey], set)
	if err != nil {
		return fmt.Errorf("changing %s of ConfigMap %s: %w", profileKey, profileConfigMap, err)
	}
	cm.Data[profileKey] = profile
	if _, err := configMaps.Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("writing the profile: %w", err)
	}

	patch := fmt.Appendf(nil, `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":%q}}}`, time.Now().UTC().Format(time.RFC3339))
	_, err = e.dynamic.Resource(platformMeshGVR).Namespace(e.Namespace).Patch(ctx, platformMesh, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("requesting a reconcile of PlatformMesh: %w", err)
	}
	return nil
}

// saveProfile writes the profile as it is now to a file in the backup
// directory, unless the file is there already: a second run must not replace
// the original with what the first one changed.
func (e *Env) saveProfile(ctx context.Context, name string) (string, error) {
	file := filepath.Join(e.BackupDir, name)
	if _, err := os.Stat(file); err == nil {
		return file, nil
	}
	cm, err := e.kube.CoreV1().ConfigMaps(e.Namespace).Get(ctx, profileConfigMap, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("reading the profile: %w", err)
	}
	if err := os.MkdirAll(e.BackupDir, 0o700); err != nil {
		return "", err
	}
	// A profile can hold credentials.
	return file, os.WriteFile(file, []byte(cm.Data[profileKey]), 0o600)
}

// waitPodReady waits until the pod is ready.
func (e *Env) waitPodReady(ctx context.Context, name string) error {
	return e.poll(ctx, func(ctx context.Context) (bool, string, error) {
		pod, err := e.kube.CoreV1().Pods(e.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err.Error(), nil //nolint:nilerr // retried, the error is the status
		}
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return true, "", nil
			}
		}
		return false, "pod " + name + " is not ready", nil
	})
}

// exec runs a command in a container of a pod, like "kubectl exec -i". What
// the command writes to its standard error is part of the error.
func (e *Env) exec(ctx context.Context, pod, container string, stdin io.Reader, stdout io.Writer, command ...string) error {
	req := e.kube.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(e.Namespace).Name(pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     stdin != nil,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(e.config, "POST", req.URL())
	if err != nil {
		return err
	}
	if stdout == nil {
		stdout = io.Discard
	}
	var stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdin: stdin, Stdout: stdout, Stderr: &stderr})
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s in pod %s: %w: %s", command[0], pod, err, msg)
		}
		return fmt.Errorf("%s in pod %s: %w", command[0], pod, err)
	}
	return nil
}

// output runs a command in a container of a pod and returns what it prints.
func (e *Env) output(ctx context.Context, pod, container string, stdin io.Reader, command ...string) (string, error) {
	var out bytes.Buffer
	if err := e.exec(ctx, pod, container, stdin, &out, command...); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// errStopped is what a migration wraps a failure in that leaves a service of
// Platform Mesh stopped, with how to get it back.
func errStopped(err error, service, profileBackup string) error {
	return errors.Join(err, fmt.Errorf(
		"%s is left stopped: fix the cause and run the migration again, or bring it back by applying the profile saved in %s",
		service, profileBackup))
}
