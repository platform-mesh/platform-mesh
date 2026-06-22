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
)

type TestApiObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status TestStatus `json:"status,omitempty"`
}
type TestStatus struct {
	Some               string
	Conditions         []metav1.Condition
	NextReconcileTime  metav1.Time
	ObservedGeneration int64
	Terminators        []string `json:"terminators,omitempty"`
	Initializers       []string `json:"initializers,omitempty"`
}

func (t *TestApiObject) DeepCopyObject() runtime.Object {
	if c := t.DeepCopy(); c != nil {
		return c
	}
	return nil
}
func (t *TestApiObject) DeepCopy() *TestApiObject {
	if t == nil {
		return nil
	}
	out := new(TestApiObject)
	t.DeepCopyInto(out)
	return out
}
func (m *TestApiObject) DeepCopyInto(out *TestApiObject) {
	*out = *m
	out.TypeMeta = m.TypeMeta
	m.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
}

type TestNoStatusApiObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

func (t *TestNoStatusApiObject) DeepCopyObject() runtime.Object {
	if c := t.DeepCopy(); c != nil {
		return c
	}
	return nil
}
func (t *TestNoStatusApiObject) DeepCopy() *TestNoStatusApiObject {
	if t == nil {
		return nil
	}
	out := new(TestNoStatusApiObject)
	t.DeepCopyInto(out)
	return out
}
func (m *TestNoStatusApiObject) DeepCopyInto(out *TestNoStatusApiObject) {
	*out = *m
	out.TypeMeta = m.TypeMeta
	m.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
}
