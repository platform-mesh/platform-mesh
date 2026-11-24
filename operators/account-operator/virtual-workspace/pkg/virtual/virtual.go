/*
Copyright 2026 The kcp Authors.

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

// Package virtual provides shared building blocks for the virtual workspaces
// served by this binary, built on the kcp virtual-workspace-framework.
package virtual

import (
	"context"

	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	genericapiserver "k8s.io/apiserver/pkg/server"

	"github.com/kcp-dev/virtual-workspace-framework/framework"
)

// AuthenticatedOnlyAuthorizer allows any authenticated, non-anonymous user.
// SCAR is a self-review — the caller only asks about themselves — so the
// access graph, not the authorizer, decides what the answer contains.
func AuthenticatedOnlyAuthorizer() authorizer.Authorizer {
	return authorizer.AuthorizerFunc(func(_ context.Context, attrs authorizer.Attributes) (authorizer.Decision, string, error) {
		u := attrs.GetUser()
		if u == nil || u.GetName() == "" || u.GetName() == user.Anonymous {
			return authorizer.DecisionDeny, "authentication required", nil
		}
		return authorizer.DecisionAllow, "available to any authenticated user", nil
	})
}

// CoreVirtualWorkspace is framework.VirtualWorkspace without the admission
// interfaces.
type CoreVirtualWorkspace interface {
	authorizer.Authorizer
	framework.RootPathResolver
	framework.ReadyChecker
	Register(name string, rootAPIServerConfig genericapiserver.CompletedConfig, delegateAPIServer genericapiserver.DelegationTarget) (genericapiserver.DelegationTarget, error)
}

// TODO: if SCAR ever grows persisted (writable) state, replace this
// with real admission — at minimum validation of the incoming object.
type WithoutAdmission struct {
	CoreVirtualWorkspace
}

var _ framework.VirtualWorkspace = &WithoutAdmission{}

func (w *WithoutAdmission) Admit(_ context.Context, _ admission.Attributes, _ admission.ObjectInterfaces) error {
	return nil
}

func (w *WithoutAdmission) Validate(_ context.Context, _ admission.Attributes, _ admission.ObjectInterfaces) error {
	return nil
}
