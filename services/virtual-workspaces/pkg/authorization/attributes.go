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

package authorization

import (
	"context"

	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

// attributeHolder is a cheat to be able to replace information in an existing
// context (instead of returning a new context with the given data). This cheat
// is used to setup an empty holder in the virtual workspace's path resolver and
// then filled in by the authorizer. Later the information is then used by the
// storage wrapper for filtering.
type attributeHolder struct {
	Attributes authorizer.Attributes
}

type attributeHolderCtxKeyType int

const attributeHolderCtxKey attributeHolderCtxKeyType = iota

func WithAttributeHolder(parent context.Context) context.Context {
	return context.WithValue(parent, attributeHolderCtxKey, &attributeHolder{})
}

func AttributesFromContext(ctx context.Context) authorizer.Attributes {
	holder, ok := ctx.Value(attributeHolderCtxKey).(*attributeHolder)
	if !ok {
		panic("Expected auth attributes to be present in context, but is not.")
	}

	return holder.Attributes
}

func UpdateAttributes(ctx context.Context, attributes authorizer.Attributes) {
	holder, ok := ctx.Value(attributeHolderCtxKey).(*attributeHolder)
	if !ok {
		return
	}
	holder.Attributes = attributes
}

// contextualAuthorizationWrapper wraps an actual authorizer and takes care of
// injecting the auth attributes into the context. It's its own wrapper to
// ensure no caching shenanigans could interfere with the correctness of the
// attributes in the current context.
type contextualAuthorizationWrapper struct {
	delegate authorizer.Authorizer
}

func NewAttributesKeeper(delegate authorizer.Authorizer) authorizer.Authorizer {
	return &contextualAuthorizationWrapper{
		delegate: delegate,
	}
}

func (w *contextualAuthorizationWrapper) Authorize(ctx context.Context, attr authorizer.Attributes) (authorized authorizer.Decision, reason string, err error) {
	UpdateAttributes(ctx, attr)

	return w.delegate.Authorize(ctx, attr)
}

// DeepCopyAttributes creates a copy of the given attributes, returned as a raw
// record to allow further modifications.
func DeepCopyAttributes(attr authorizer.Attributes) authorizer.AttributesRecord {
	return authorizer.AttributesRecord{
		User: &user.DefaultInfo{
			Name:   attr.GetUser().GetName(),
			UID:    attr.GetUser().GetUID(),
			Groups: attr.GetUser().GetGroups(),
			Extra:  attr.GetUser().GetExtra(),
		},
		Verb:            attr.GetVerb(),
		Namespace:       attr.GetNamespace(),
		APIGroup:        attr.GetAPIGroup(),
		APIVersion:      attr.GetAPIVersion(),
		Resource:        attr.GetResource(),
		Subresource:     attr.GetSubresource(),
		Name:            attr.GetName(),
		ResourceRequest: attr.IsResourceRequest(),
		Path:            attr.GetPath(),
	}
}
