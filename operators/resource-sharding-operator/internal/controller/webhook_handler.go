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

package controller

import (
	"context"
	"encoding/json"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type ShardAssignHandler struct {
	Registry *DynamicControllerRegistry
}

func (h *ShardAssignHandler) Handle(_ context.Context, req admission.Request) admission.Response {
	logger := ctrl.Log.WithName("webhook").WithValues("resource", req.Name, "namespace", req.Namespace)

	if req.Operation != admissionv1.Create {
		return admission.Allowed("")
	}

	running := h.Registry.FindByGVR(req.Resource.Group, req.Resource.Version, req.Resource.Resource)
	if running == nil {
		return admission.Allowed("no matching ResourceSharding")
	}

	labels := extractLabels(req.Object.Raw)
	if _, exists := labels[running.LabelKey]; exists {
		return admission.Allowed("already labeled")
	}

	shard := running.Assigner.Next()
	if shard == "" {
		return admission.Allowed("no shards configured")
	}
	logger.Info("webhook assigning shard", "shard", shard)

	var patch []map[string]any
	if labels == nil {
		patch = []map[string]any{
			{
				"op":    "add",
				"path":  "/metadata/labels",
				"value": map[string]string{running.LabelKey: shard}, //nolint:goconst
			},
		}
	} else {
		patch = []map[string]any{
			{
				"op":    "add",
				"path":  "/metadata/labels/" + escapeJSONPointer(running.LabelKey),
				"value": shard,
			},
		}
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	resp := admission.Allowed("shard assigned")
	patchType := admissionv1.PatchTypeJSONPatch
	resp.PatchType = &patchType
	resp.Patch = patchBytes
	return resp
}

func extractLabels(raw []byte) map[string]string {
	var obj struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	_ = json.Unmarshal(raw, &obj)
	return obj.Metadata.Labels
}

func escapeJSONPointer(s string) string {
	result := ""
	for _, c := range s {
		switch c {
		case '~':
			result += "~0"
		case '/':
			result += "~1"
		default:
			result += string(c)
		}
	}
	return result
}
