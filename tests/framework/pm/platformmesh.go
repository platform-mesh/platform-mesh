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

package pm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Namespace is where Platform Mesh is installed, and Name the name of
	// its PlatformMesh object there.
	Namespace = "platform-mesh-system"
	Name      = "platform-mesh"

	// AdminKubeconfigSecret is the Secret in Namespace the operator keeps
	// the kubeconfig of the kcp admin in, and AdminContext the context of
	// that kubeconfig that points at the front-proxy without a workspace.
	AdminKubeconfigSecret = "kubeconfig-kcp-admin"
	AdminContext          = "base"

	// kubeconfigKey is the key of a Secret the operator writes a
	// kubeconfig under.
	kubeconfigKey = "kubeconfig"
)

// PlatformMeshKey is the key of the PlatformMesh object.
func PlatformMeshKey() ctrlruntimeclient.ObjectKey {
	return ctrlruntimeclient.ObjectKey{Namespace: Namespace, Name: Name}
}

// GetPlatformMesh reads the PlatformMesh object with cl, a client for the
// cluster Platform Mesh is installed into.
func GetPlatformMesh(ctx context.Context, cl ctrlruntimeclient.Client) (*pmcorev1alpha1.PlatformMesh, error) {
	mesh := &pmcorev1alpha1.PlatformMesh{}
	if err := cl.Get(ctx, PlatformMeshKey(), mesh); err != nil {
		return nil, fmt.Errorf("getting PlatformMesh: %w", err)
	}
	return mesh, nil
}

// CheckPlatformMeshReady returns the PlatformMesh object, and an error
// wrapping wait.ErrNotReady unless it is Ready for what it says now.
func CheckPlatformMeshReady(ctx context.Context, cl ctrlruntimeclient.Client) (*pmcorev1alpha1.PlatformMesh, error) {
	mesh, err := GetPlatformMesh(ctx, cl)
	if err != nil {
		return nil, err
	}
	if err := wait.CheckReadyAt(mesh.Status.Conditions, mesh.Generation); err != nil {
		return mesh, fmt.Errorf("PlatformMesh: %w", err)
	}
	return mesh, nil
}

// AppendToPlatformMesh appends an entry to a list of the PlatformMesh
// object, for example spec.kcp.extraWorkspaces as
// AppendToPlatformMesh(ctx, cl, entry, "spec", "kcp", "extraWorkspaces").
//
// The object is patched, with the list alone, and the entries that are
// there are read as they are and not through their Go type. A test may run
// against another version of Platform Mesh than the one its Go types are
// from, and an update through the type drops every field that version has
// and the type does not know, of an object that configures the whole
// installation.
//
// It fails with a conflict when the object changes in between; call it
// again.
func AppendToPlatformMesh(ctx context.Context, cl ctrlruntimeclient.Client, entry any, list ...string) error {
	raw := &unstructured.Unstructured{}
	raw.SetGroupVersionKind(pmcorev1alpha1.SchemeGroupVersion.WithKind("PlatformMesh"))
	if err := cl.Get(ctx, PlatformMeshKey(), raw); err != nil {
		return fmt.Errorf("getting PlatformMesh: %w", err)
	}
	field := strings.Join(list, ".")
	entries, _, err := unstructured.NestedSlice(raw.Object, list...)
	if err != nil {
		return fmt.Errorf("%s of PlatformMesh: %w", field, err)
	}
	// A merge patch replaces a list as a whole, so it carries the entries
	// that are there, and the resourceVersion they were read at.
	var value any = append(entries, entry)
	for i := len(list) - 1; i >= 0; i-- {
		value = map[string]any{list[i]: value}
	}
	body := value.(map[string]any)
	body["metadata"] = map[string]any{"resourceVersion": raw.GetResourceVersion()}
	patch, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if err := cl.Patch(ctx, raw, ctrlruntimeclient.RawPatch(types.MergePatchType, patch)); err != nil {
		return fmt.Errorf("appending to %s of PlatformMesh: %w", field, err)
	}
	return nil
}

// SecretKubeconfig returns the kubeconfig the operator wrote into the
// Secret name in Namespace, or an error while there is none.
func SecretKubeconfig(ctx context.Context, cl ctrlruntimeclient.Client, name string) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: Namespace, Name: name}, secret); err != nil {
		return nil, fmt.Errorf("getting secret %s: %w", name, err)
	}
	if len(secret.Data[kubeconfigKey]) == 0 {
		return nil, fmt.Errorf("secret %s has no %s", name, kubeconfigKey)
	}
	return secret.Data[kubeconfigKey], nil
}

// AdminConfig is the kcp admin config, without a workspace path, of a
// kubeconfig as the operator keeps it in AdminKubeconfigSecret. It points at
// the front-proxy as the host of the test reaches it.
func AdminConfig(kubeconfig []byte) (*rest.Config, error) {
	raw, err := clientcmd.Load(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("parsing the kcp admin kubeconfig: %w", err)
	}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(*raw, AdminContext, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("the %s context of the kcp admin kubeconfig: %w", AdminContext, err)
	}
	cfg.Host = strings.TrimSuffix(cfg.Host, "/")
	return cfg, nil
}

// Reachable is the config of a kubeconfig a component wrote for use inside
// the cluster, made to work from the host of the test. Its server (the
// front-proxy Service, or a virtual workspace URL) is replaced by the
// front-proxy of kcp, the admin config of the installation; a /services/
// virtual workspace path becomes /clusters/<fallbackPath>. The credentials
// and the CA data are the kubeconfig's.
func Reachable(kcp *rest.Config, kubeconfig []byte, fallbackPath string) (*rest.Config, error) {
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("rest config from kubeconfig: %w", err)
	}
	server, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("parsing server %q: %w", cfg.Host, err)
	}
	front, err := url.Parse(kcp.Host)
	if err != nil {
		return nil, fmt.Errorf("parsing the front-proxy URL: %w", err)
	}
	server.Scheme, server.Host = front.Scheme, front.Host
	if strings.HasPrefix(server.Path, "/services/") {
		server.Path = "/clusters/" + fallbackPath
	}
	cfg.Host = server.String()
	return cfg, nil
}
