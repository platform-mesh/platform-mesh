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

package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kcpPolicyKeyPrefix    = "platform-mesh/kcp-apiexport-policies"
	kcpWorkspaceKeyPrefix = "platform-mesh/kcp-workspaces"
)

var (
	kcpPolicyGVR    = schema.GroupVersionResource{Group: "core.platform-mesh.io", Version: "v1alpha1", Resource: "apiexportpolicies"}
	kcpWorkspaceGVR = schema.GroupVersionResource{Group: "tenancy.kcp.io", Version: "v1alpha1", Resource: "workspaces"}
)

type kcpPolicyManifest map[string][]map[string]any

type KCPPolicyCaptureSubroutine struct{ client ctrlruntimeclient.Client }

func NewKCPPolicyCaptureSubroutine(client ctrlruntimeclient.Client) *KCPPolicyCaptureSubroutine {
	return &KCPPolicyCaptureSubroutine{client: client}
}
func (p *KCPPolicyCaptureSubroutine) GetName() string { return "kcp-policy-capture" }

func (p *KCPPolicyCaptureSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	b, ok := obj.(*pmbackupv1alpha1.PlatformBackup)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected PlatformBackup, got %T", obj)
	}
	store, err := (&OpenFGADumpSubroutine{client: p.client}).s3(ctx, b.Spec.Storage)
	if err != nil {
		return subroutines.OK(), false, err
	}
	policyKey := fmt.Sprintf("%s/%s.json", kcpPolicyKeyPrefix, b.Name)
	workspaceKey := fmt.Sprintf("%s/%s.json", kcpWorkspaceKeyPrefix, b.Name)
	_, policyExists := store.StatObject(ctx, cnpgBackupBucket, policyKey, minio.StatObjectOptions{})
	_, workspaceExists := store.StatObject(ctx, cnpgBackupBucket, workspaceKey, minio.StatObjectOptions{})
	if policyExists == nil && workspaceExists == nil {
		return subroutines.OK(), false, nil
	}
	if policyExists != nil {
		manifest := kcpPolicyManifest{}
		for _, path := range []string{"root:orgs", "root:platform-mesh-system"} {
			client, err := p.kcpClient(ctx, path)
			if err != nil {
				return subroutines.OK(), false, err
			}
			items, err := client.Resource(kcpPolicyGVR).List(ctx, metav1.ListOptions{})
			if err != nil {
				return subroutines.OK(), false, fmt.Errorf("list APIExportPolicies in %s: %w", path, err)
			}
			for _, item := range items.Items {
				name := item.GetName()
				if name == "" {
					return subroutines.OK(), false, fmt.Errorf("APIExportPolicy in %s has no metadata.name", path)
				}
				delete(item.Object, "status")
				delete(item.Object, "metadata")
				item.Object["metadata"] = map[string]any{"name": name}
				manifest[path] = append(manifest[path], item.Object)
			}
		}
		raw, err := json.Marshal(manifest)
		if err != nil {
			return subroutines.OK(), false, err
		}
		if _, err = store.PutObject(ctx, cnpgBackupBucket, policyKey, strings.NewReader(string(raw)), int64(len(raw)), minio.PutObjectOptions{ContentType: "application/json"}); err != nil {
			return subroutines.OK(), false, fmt.Errorf("upload KCP APIExportPolicy manifest: %w", err)
		}
	}
	if workspaceExists != nil {
		client, err := p.kcpRootClient(ctx)
		if err != nil {
			return subroutines.OK(), false, err
		}
		workspace, err := client.Resource(kcpWorkspaceGVR).Get(ctx, "orgs", metav1.GetOptions{})
		if err != nil {
			return subroutines.OK(), false, fmt.Errorf("get KCP Workspace orgs: %w", err)
		}
		delete(workspace.Object, "status")
		delete(workspace.Object, "metadata")
		workspace.Object["metadata"] = map[string]any{"name": "orgs"}
		raw, err := json.Marshal(workspace.Object)
		if err != nil {
			return subroutines.OK(), false, err
		}
		if _, err = store.PutObject(ctx, cnpgBackupBucket, workspaceKey, strings.NewReader(string(raw)), int64(len(raw)), minio.PutObjectOptions{ContentType: "application/json"}); err != nil {
			return subroutines.OK(), false, fmt.Errorf("upload KCP Workspace manifest: %w", err)
		}
	}
	return subroutines.OK(), false, nil
}

func (p *KCPPolicyCaptureSubroutine) kcpClient(ctx context.Context, path string) (dynamic.Interface, error) {
	var secret corev1.Secret
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: "platform-mesh-system", Name: "kubeconfig-kcp-admin"}, &secret); err != nil {
		return nil, err
	}
	cfg, err := clientcmd.RESTConfigFromKubeConfig(secret.Data["kubeconfig"])
	if err != nil {
		return nil, err
	}
	base := os.Getenv("KCP_BASE_URL")
	if base == "" {
		base = "https://frontproxy-front-proxy.platform-mesh-system:8443"
	}
	cfg.Host = strings.TrimRight(base, "/") + "/clusters/" + path
	cfg.APIPath = ""
	return dynamic.NewForConfig(cfg)
}

func (p *KCPPolicyCaptureSubroutine) kcpRootClient(ctx context.Context) (dynamic.Interface, error) {
	var clientSecret, caSecret corev1.Secret
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: "platform-mesh-system", Name: "root-client"}, &clientSecret); err != nil {
		return nil, err
	}
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: "platform-mesh-system", Name: "root-server-ca"}, &caSecret); err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(&rest.Config{
		Host:            "https://root-kcp.platform-mesh-system.svc.cluster.local:6443/clusters/root",
		TLSClientConfig: rest.TLSClientConfig{CertData: clientSecret.Data["tls.crt"], KeyData: clientSecret.Data["tls.key"], CAData: caSecret.Data["tls.crt"]},
		Timeout:         30 * time.Second,
	})
}
