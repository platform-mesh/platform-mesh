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

package v1alpha1

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// +kubebuilder:rbac:groups=broker.platform-mesh.io,resources=acceptapis,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=broker.platform-mesh.io,resources=acceptapis/status,verbs=get
// +kubebuilder:rbac:groups=broker.platform-mesh.io,resources=acceptapis/finalizers,verbs=update

// AcceptAPISpec defines the desired state of AcceptAPI.
type AcceptAPISpec struct {
	// GVR is the GroupVersionResource of the API to accept.
	// +kubebuilder:validation:Required
	GVR metav1.GroupVersionResource `json:"gvr"`

	// APIExportName is the name of the APIExport in the provider's
	// workspace that serves the accepted GVR.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	APIExportName string `json:"apiExportName"`

	// Filters to select which resources of the GVR to accept.
	Filters []Filter `json:"filters,omitempty"`
}

// Filter defines a filter to select resources.
type Filter struct {
	// Key is the spec key to filter on.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="!self.startsWith('spec.')",message="Key must not start with 'spec.' as it is implied"
	Key string `json:"key"`

	// ValueIn is the list of allowed values for the key.
	// +optional
	ValueIn []string `json:"valueIn,omitempty"`

	// Boundary defines min and/or max boundaries for numeric filtering.
	// +optional
	Boundary Boundary `json:"boundary,omitempty"`

	// Suffix is the required suffix for string filtering.
	// +optional
	Suffix string `json:"suffix,omitempty"`
}

// Boundary defines a min and max boundary for numeric filtering.
type Boundary struct {
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
}

// AppliesTo checks if the given object matches the filters.
func (acceptAPI *AcceptAPI) AppliesTo(gvr metav1.GroupVersionResource, obj *unstructured.Unstructured) (bool, []AcceptAPIDenyReason) {
	if acceptAPI.Spec.GVR.String() != gvr.String() {
		return false, []AcceptAPIDenyReason{
			{
				Key:    "gvr",
				Reason: "GVR does not match",
				Rule:   acceptAPI.Spec.GVR.String(),
				Value:  gvr.String(),
			},
		}
	}

	reasons := []AcceptAPIDenyReason{}

	for _, filter := range acceptAPI.Spec.Filters {
		keyParts := strings.Split(filter.Key, ".")
		fields := make([]string, 0, 1+len(keyParts))
		fields = append(fields, "spec")
		fields = append(fields, keyParts...)

		valRaw, found, err := unstructured.NestedFieldNoCopy(obj.Object, fields...)
		if err != nil || !found {
			continue
		}

		// Convert to string for consistent comparison across different numeric and string types.
		val := fmt.Sprintf("%v", valRaw)

		// very rudimentary and most certainly not to spec, but it's
		// enough for a poc

		if len(filter.ValueIn) > 0 {
			if !slices.Contains(filter.ValueIn, val) {
				reasons = append(reasons, AcceptAPIDenyReason{
					Key:    filter.Key,
					Reason: "Value not in allowed set",
					Rule:   strings.Join(filter.ValueIn, ","),
					Value:  val,
				})
			}
		}

		if filter.Boundary.Min != nil && *filter.Boundary.Min >= 0 {
			numVal, err := strconv.Atoi(val)
			if err != nil || numVal < *filter.Boundary.Min {
				reasons = append(reasons, AcceptAPIDenyReason{
					Key:    filter.Key,
					Reason: "Value below minimum boundary",
					Rule:   strconv.Itoa(*filter.Boundary.Min),
					Value:  val,
				})
			}
		}

		if filter.Boundary.Max != nil && *filter.Boundary.Max >= 0 {
			numVal, err := strconv.Atoi(val)
			if err != nil || numVal > *filter.Boundary.Max {
				reasons = append(reasons, AcceptAPIDenyReason{
					Key:    filter.Key,
					Reason: "Value above maximum boundary",
					Rule:   strconv.Itoa(*filter.Boundary.Max),
					Value:  val,
				})
			}
		}

		if filter.Suffix != "" {
			if !strings.HasSuffix(val, filter.Suffix) {
				reasons = append(reasons, AcceptAPIDenyReason{
					Key:    filter.Key,
					Reason: "Value does not have required suffix",
					Rule:   filter.Suffix,
					Value:  val,
				})
			}
		}
	}

	return len(reasons) == 0, reasons
}

// AcceptAPIDenyReason is returned by AcceptAPI when it denies an
// object.
type AcceptAPIDenyReason struct {
	// Key is the key of the field that caused the denial.
	Key string
	// Reason is a human-readable reason for the denial.
	Reason string
	// Value is the value of the field that caused the denial.
	Value string
	// Rule is the rule that was checked against, e.g. the GVR or the
	// list of allowed values.
	Rule string
}

// AcceptAPIStatus defines the observed state of AcceptAPI.
type AcceptAPIStatus struct {
	// ProviderPath is the resolved kcp workspace path of the provider's
	// workspace (where the AcceptAPI object lives). Set by the broker.
	// +optional
	ProviderPath string `json:"providerPath,omitempty"`

	// conditions represent the current state of the AcceptAPI resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AcceptAPI is the Schema for the acceptapis API.
type AcceptAPI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AcceptAPISpec   `json:"spec,omitempty"`
	Status AcceptAPIStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AcceptAPIList contains a list of AcceptAPI.
type AcceptAPIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AcceptAPI `json:"items"`
}
