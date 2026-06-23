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

package apidefinition

import (
	"context"
	"fmt"

	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapidefinition "github.com/kcp-dev/virtual-workspace-framework/pkg/dynamic/apidefinition"
	"github.com/kcp-dev/virtual-workspace-framework/pkg/dynamic/apiserver"
	dynamiccontext "github.com/kcp-dev/virtual-workspace-framework/pkg/dynamic/context"

	"k8s.io/apimachinery/pkg/runtime/schema"
	genericapiserver "k8s.io/apiserver/pkg/server"
)

type StorageProviderFunc func(ctx context.Context) (apiserver.RestProviderFunc, error)

type singleResourceAPIDefinitionSetProvider struct {
	config          genericapiserver.CompletedConfig
	gvr             schema.GroupVersionResource
	resource        *apisv1alpha1.APIResourceSchema
	storageProvider StorageProviderFunc
}

func NewSingleResourceProvider(
	config genericapiserver.CompletedConfig,
	gvr schema.GroupVersionResource,
	resource *apisv1alpha1.APIResourceSchema,
	storageProvider StorageProviderFunc,
) kcpapidefinition.APIDefinitionSetGetter {
	return &singleResourceAPIDefinitionSetProvider{
		config:          config,
		gvr:             gvr,
		resource:        resource,
		storageProvider: storageProvider,
	}
}

func (a *singleResourceAPIDefinitionSetProvider) GetAPIDefinitionSet(ctx context.Context, _ dynamiccontext.APIDomainKey) (apis kcpapidefinition.APIDefinitionSet, apisExist bool, err error) {
	restProvider, err := a.storageProvider(ctx)
	if err != nil {
		return nil, false, err
	}

	apiDefinition, err := apiserver.CreateServingInfoFor(
		a.config,
		a.resource,
		a.gvr.Version,
		restProvider,
	)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create serving info: %w", err)
	}

	apis = kcpapidefinition.APIDefinitionSet{
		a.gvr: apiDefinition,
	}

	return apis, len(apis) > 0, nil
}

var _ kcpapidefinition.APIDefinitionSetGetter = &singleResourceAPIDefinitionSetProvider{}
