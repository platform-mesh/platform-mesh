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

package fields

import (
	"github.com/graphql-go/graphql"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ResourceContext struct {
	GVK            schema.GroupVersionKind
	Scope          apiextensionsv1.ResourceScope
	UniqueTypeName string
	ResourceType   *graphql.Object
	InputType      *graphql.InputObject
	SingularName   string
	PluralName     string
	SanitizedGroup string
}

func (r *ResourceContext) IsNamespaceScoped() bool {
	return r.Scope == apiextensionsv1.NamespaceScoped
}
