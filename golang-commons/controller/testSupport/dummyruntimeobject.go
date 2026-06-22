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

package testSupport

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type DummyRuntimeObject struct{}

func (d DummyRuntimeObject) GetObjectKind() schema.ObjectKind              { return nil }
func (d DummyRuntimeObject) DeepCopyObject() runtime.Object                { return nil }
func (d DummyRuntimeObject) GetAnnotations() map[string]string             { return nil }
func (d DummyRuntimeObject) SetAnnotations(map[string]string)              {}
func (d DummyRuntimeObject) GetCreationTimestamp() metav1.Time             { return metav1.Time{} }
func (d DummyRuntimeObject) SetCreationTimestamp(metav1.Time)              {}
func (d DummyRuntimeObject) GetNamespace() string                          { return "" }
func (d DummyRuntimeObject) SetNamespace(string)                           {}
func (d DummyRuntimeObject) GetName() string                               { return "" }
func (d DummyRuntimeObject) SetName(string)                                {}
func (d DummyRuntimeObject) GetGenerateName() string                       { return "" }
func (d DummyRuntimeObject) SetGenerateName(string)                        {}
func (d DummyRuntimeObject) GetUID() types.UID                             { return "" }
func (d DummyRuntimeObject) SetUID(types.UID)                              {}
func (d DummyRuntimeObject) GetGeneration() int64                          { return 0 }
func (d DummyRuntimeObject) SetGeneration(int64)                           {}
func (d DummyRuntimeObject) GetResourceVersion() string                    { return "" }
func (d DummyRuntimeObject) SetResourceVersion(string)                     {}
func (d DummyRuntimeObject) GetFinalizers() []string                       { return nil }
func (d DummyRuntimeObject) SetFinalizers([]string)                        {}
func (d DummyRuntimeObject) GetLabels() map[string]string                  { return nil }
func (d DummyRuntimeObject) SetLabels(map[string]string)                   {}
func (d DummyRuntimeObject) GetOwnerReferences() []metav1.OwnerReference   { return nil }
func (d DummyRuntimeObject) SetOwnerReferences([]metav1.OwnerReference)    {}
func (d DummyRuntimeObject) GetManagedFields() []metav1.ManagedFieldsEntry { return nil }
func (d DummyRuntimeObject) SetManagedFields([]metav1.ManagedFieldsEntry)  {}
func (d DummyRuntimeObject) GetSelfLink() string                           { return "" }
func (d DummyRuntimeObject) SetSelfLink(string)                            {}
func (d DummyRuntimeObject) GetClusterName() string                        { return "" }
func (d DummyRuntimeObject) SetClusterName(string)                         {}
func (d DummyRuntimeObject) GetDeletionTimestamp() *metav1.Time            { return nil }
func (d DummyRuntimeObject) SetDeletionTimestamp(*metav1.Time)             {}
func (d DummyRuntimeObject) GetDeletionGracePeriodSeconds() *int64         { return nil }
func (d DummyRuntimeObject) SetDeletionGracePeriodSeconds(*int64)          {}

type DummyRuntimeObjectWithConditions struct {
	DummyRuntimeObject
}

func (d DummyRuntimeObjectWithConditions) GetConditions() []metav1.Condition { return nil }
func (d DummyRuntimeObjectWithConditions) SetConditions([]metav1.Condition)  {}
