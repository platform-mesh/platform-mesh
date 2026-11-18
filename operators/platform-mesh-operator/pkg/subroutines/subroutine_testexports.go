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
	"io/fs"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Test hooks for private functions (used by tests in package subroutines).

func (r *KcpsetupSubroutine) GetCaBundle(ctx context.Context, webhookConfig *pmcorev1alpha1.WebhookConfiguration) ([]byte, error) {
	return r.getCaBundle(ctx, webhookConfig)
}

func (r *KcpsetupSubroutine) GetCABundleInventory(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh) (map[string]string, error) {
	return r.getCABundleInventory(ctx, inst)
}

func (r *KcpsetupSubroutine) CreateKcpResources(ctx context.Context, config *rest.Config, inst *pmcorev1alpha1.PlatformMesh) error {
	return r.createKcpResources(ctx, config, inst)
}

func (r *KcpsetupSubroutine) GetAPIExportHashInventory(ctx context.Context, config *rest.Config) (map[string]string, error) {
	return r.getAPIExportHashInventory(ctx, config)
}

func (s *DeploymentSubroutine) ApplyManifestFromFileWithMergedValues(ctx context.Context, fsys fs.FS, path string, k8sClient ctrlruntimeclient.Client, templateData map[string]any) error {
	return applyManifestFromFileWithMergedValues(ctx, fsys, path, k8sClient, templateData)
}

func (r *KcpsetupSubroutine) UnstructuredFromFile(fsys fs.FS, path string, templateData map[string]any, log *logger.Logger) (unstructured.Unstructured, error) {
	return unstructuredFromFS(fsys, path, templateData, log)
}

func (r *KcpsetupSubroutine) ApplyExtraWorkspaces(ctx context.Context, config *rest.Config, inst *pmcorev1alpha1.PlatformMesh) error {
	return r.applyExtraWorkspaces(ctx, config, inst)
}
