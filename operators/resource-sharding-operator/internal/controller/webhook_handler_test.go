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

//nolint:goconst
package controller

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeRequest constructs a minimal admission.Request.
//
//nolint:unparam // version is currently always "v1" but kept as a parameter for future tests
func makeRequest(op admissionv1.Operation, group, version, resource string, rawObj []byte) admission.Request {
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       types.UID("test-uid"),
			Operation: op,
			Resource: metav1.GroupVersionResource{
				Group:    group,
				Version:  version,
				Resource: resource,
			},
			Object: runtime.RawExtension{Raw: rawObj},
		},
	}
}

// makeObjectJSON returns minimal Kubernetes object JSON.
func makeObjectJSON(t *testing.T, labels map[string]string) []byte {
	t.Helper()
	obj := map[string]any{
		"metadata": map[string]any{},
	}
	if labels != nil {
		obj["metadata"].(map[string]any)["labels"] = labels
	}
	raw, err := json.Marshal(obj)
	require.NoError(t, err)
	return raw
}

// makeRegistry builds a DynamicControllerRegistry pre-populated with one entry.
//
//nolint:unparam // labelKey is currently always "shard.io/shard" but kept as a parameter for future tests
func makeRegistry(gvr schema.GroupVersionResource, labelKey string, shards []string) *DynamicControllerRegistry {
	reg := NewDynamicControllerRegistry()
	rc := &RunningController{
		Cancel:   func() {},
		GVR:      gvr,
		LabelKey: labelKey,
		Assigner: NewShardAssigner(shards),
	}
	reg.Register(types.UID("uid-1"), rc)
	return reg
}

// patchOps decodes the JSON patch operations from an admission response.
func patchOps(t *testing.T, resp admission.Response) []map[string]any {
	t.Helper()
	var ops []map[string]any
	if len(resp.Patch) == 0 {
		return ops
	}
	require.NoError(t, json.Unmarshal(resp.Patch, &ops))
	return ops
}

// ---------------------------------------------------------------------------
// escapeJSONPointer — pure function, table driven
// ---------------------------------------------------------------------------

func TestEscapeJSONPointer(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"normal", "normal"},
		{"~", "~0"},
		{"/", "~1"},
		{"~/", "~0~1"},
		{"/~/", "~1~0~1"},
		{"a/b~c", "a~1b~0c"},
		{"shard.io/key", "shard.io~1key"},
		{"a~~b//c", "a~0~0b~1~1c"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := escapeJSONPointer(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// ShardAssignHandler.Handle
// ---------------------------------------------------------------------------

func TestShardAssignHandler_Handle_CreateWithoutLabel_AssignsShard(t *testing.T) {
	const labelKey = "shard.io/shard"
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	reg := makeRegistry(gvr, labelKey, []string{"shard-a", "shard-b"})
	handler := &ShardAssignHandler{Registry: reg}

	raw := makeObjectJSON(t, nil) // no labels at all
	req := makeRequest(admissionv1.Create, "", "v1", "configmaps", raw)

	resp := handler.Handle(context.Background(), req)

	assert.True(t, resp.Allowed, "response should be allowed")
	ops := patchOps(t, resp)
	require.NotEmpty(t, ops, "expected at least one patch operation")

	op := ops[0]
	assert.Equal(t, "add", op["op"])
	// When there were no labels the patch adds the whole labels object
	assert.Equal(t, "/metadata/labels", op["path"])

	val, ok := op["value"].(map[string]any)
	require.True(t, ok, "patch value should be a map")
	shard, exists := val[labelKey]
	require.True(t, exists, "patch value should contain labelKey %q", labelKey)
	assert.Contains(t, []any{"shard-a", "shard-b"}, shard)
}

func TestShardAssignHandler_Handle_CreateWithExistingLabel_PassThrough(t *testing.T) {
	const labelKey = "shard.io/shard"
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	reg := makeRegistry(gvr, labelKey, []string{"shard-a"})
	handler := &ShardAssignHandler{Registry: reg}

	raw := makeObjectJSON(t, map[string]string{labelKey: "existing-shard"})
	req := makeRequest(admissionv1.Create, "", "v1", "configmaps", raw)

	resp := handler.Handle(context.Background(), req)

	assert.True(t, resp.Allowed)
	assert.Empty(t, resp.Patch, "no patch should be emitted when label already exists")
}

func TestShardAssignHandler_Handle_CreateWithExistingLabels_PatchesLabelKey(t *testing.T) {
	// Object has labels but NOT the shard label → patch should add the key to existing labels map.
	const labelKey = "shard.io/shard"
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	reg := makeRegistry(gvr, labelKey, []string{"shard-a"})
	handler := &ShardAssignHandler{Registry: reg}

	raw := makeObjectJSON(t, map[string]string{"other-key": "other-value"})
	req := makeRequest(admissionv1.Create, "", "v1", "configmaps", raw)

	resp := handler.Handle(context.Background(), req)

	assert.True(t, resp.Allowed)
	ops := patchOps(t, resp)
	require.NotEmpty(t, ops)

	op := ops[0]
	assert.Equal(t, "add", op["op"])
	// Should add just the shard label key using escaped JSON pointer
	escapedKey := escapeJSONPointer(labelKey)
	assert.Equal(t, "/metadata/labels/"+escapedKey, op["path"])
	assert.Equal(t, "shard-a", op["value"])
}

func TestShardAssignHandler_Handle_NonCreateOp_PassThrough(t *testing.T) {
	tests := []struct {
		name string
		op   admissionv1.Operation
	}{
		{"UPDATE", admissionv1.Update},
		{"DELETE", admissionv1.Delete},
		{"CONNECT", admissionv1.Connect},
	}

	const labelKey = "shard.io/shard"
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	reg := makeRegistry(gvr, labelKey, []string{"shard-a"})
	handler := &ShardAssignHandler{Registry: reg}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := makeObjectJSON(t, nil)
			req := makeRequest(tc.op, "", "v1", "configmaps", raw)

			resp := handler.Handle(context.Background(), req)

			assert.True(t, resp.Allowed, "non-CREATE ops should be allowed")
			assert.Empty(t, resp.Patch, "no patch for non-CREATE ops")
		})
	}
}

func TestShardAssignHandler_Handle_ResourceNotInRegistry_PassThrough(t *testing.T) {
	reg := NewDynamicControllerRegistry() // empty registry
	handler := &ShardAssignHandler{Registry: reg}

	raw := makeObjectJSON(t, nil)
	req := makeRequest(admissionv1.Create, "apps", "v1", "deployments", raw)

	resp := handler.Handle(context.Background(), req)

	assert.True(t, resp.Allowed)
	assert.Empty(t, resp.Patch, "no patch when resource not in registry")
}

func TestShardAssignHandler_Handle_EmptyAssignerShards_PassThrough(t *testing.T) {
	// Registry has an entry but assigner has no shards → should allow without patch, no panic.
	const labelKey = "shard.io/shard"
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	reg := makeRegistry(gvr, labelKey, nil) // nil shards → assigner returns ""
	handler := &ShardAssignHandler{Registry: reg}

	raw := makeObjectJSON(t, nil)
	req := makeRequest(admissionv1.Create, "", "v1", "configmaps", raw)

	require.NotPanics(t, func() {
		resp := handler.Handle(context.Background(), req)
		assert.True(t, resp.Allowed)
		assert.Empty(t, resp.Patch)
	})
}

func TestShardAssignHandler_Handle_LabelKeyWithSlash_CorrectlyEscaped(t *testing.T) {
	// Label key containing "/" must be properly escaped in the JSON Pointer path.
	const labelKey = "shard.io/shard"
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	reg := makeRegistry(gvr, labelKey, []string{"my-shard"})
	handler := &ShardAssignHandler{Registry: reg}

	// Provide existing (but different) labels so the single-key patch path is used.
	raw := makeObjectJSON(t, map[string]string{"existing": "label"})
	req := makeRequest(admissionv1.Create, "apps", "v1", "deployments", raw)

	resp := handler.Handle(context.Background(), req)

	assert.True(t, resp.Allowed)
	ops := patchOps(t, resp)
	require.NotEmpty(t, ops)

	// "shard.io/shard" → "shard.io~1shard"
	assert.Equal(t, "/metadata/labels/shard.io~1shard", ops[0]["path"])
}
