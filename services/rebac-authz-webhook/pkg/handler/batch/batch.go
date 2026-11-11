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

package batch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-logr/logr"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	"go.platform-mesh.io/rebac-authz-webhook/pkg/clustercache"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/contextual"

	authorizationv1 "k8s.io/api/authorization/v1"
)

// ErrStoreIDMismatch is returned when items in a batch request have different store IDs.
var ErrStoreIDMismatch = fmt.Errorf("store ID mismatch")

// BatchAuthzResult represents the result of a single authorization check.
type BatchAuthzResult struct {
	// ID is the correlation ID from the request.
	ID string `json:"id"`
	// Allowed indicates whether the request is authorized.
	Allowed bool `json:"allowed"`
}

// BatchHandler handles batch authorization requests.
type BatchHandler struct {
	fga            openfgav1.OpenFGAServiceClient
	clusterCache   clustercache.Provider
	clusterPathKey string
	log            logr.Logger
}

// New creates a new BatchHandler.
func New(log logr.Logger, fga openfgav1.OpenFGAServiceClient, clusterCache clustercache.Provider, clusterPathKey string) *BatchHandler {
	return &BatchHandler{
		fga:            fga,
		clusterCache:   clusterCache,
		clusterPathKey: clusterPathKey,
		log:            log.WithName("batch-authz"),
	}
}

// ServeHTTP implements http.Handler.
func (h *BatchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Body == nil || r.Body == http.NoBody {
		h.writeError(w, http.StatusBadRequest, "request body is empty")
		return
	}
	defer r.Body.Close() //nolint:errcheck

	// 1 MiB cap prevents request body resource exhaustion and a single SAR is well under 1 KiB.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1*1024*1024))
	if err != nil {
		h.log.Error(err, "failed to read request body")
		h.writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var sars []authorizationv1.SubjectAccessReview
	if err := json.Unmarshal(body, &sars); err != nil {
		h.log.Error(err, "failed to unmarshal request")
		h.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Validate that all reviews have required fields
	for i, sar := range sars {
		if sar.Name == "" || sar.Spec.ResourceAttributes == nil || sar.Spec.User == "" || len(sar.Spec.Extra[h.clusterPathKey]) == 0 {
			h.writeError(w, http.StatusBadRequest, fmt.Sprintf("item %d has invalid input", i))
			return
		}
	}

	response, err := h.processBatch(ctx, sars)
	if err != nil {
		if errors.Is(err, ErrStoreIDMismatch) {
			h.writeError(w, http.StatusBadRequest, "store ID mismatch")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "batch check failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.log.Error(err, "failed to encode response")
	}
}

// processBatch processes all items in the batch request.
func (h *BatchHandler) processBatch(ctx context.Context, sars []authorizationv1.SubjectAccessReview) ([]BatchAuthzResult, error) {
	// Initialize results with all items denied by default
	results := make(map[string]bool, len(sars))
	for _, sar := range sars {
		results[sar.Name] = false
	}

	// build openfga check items with contextual tuples
	checks, storeID, err := h.buildChecks(sars)
	if err != nil {
		h.log.Error(err, "failed to build checks")
		return nil, err
	}

	if len(checks) == 0 || storeID == "" {
		h.log.Error(nil, "no valid checks could be built", "storeID", storeID, "checksCount", len(checks))
		return nil, fmt.Errorf("no valid checks could be built")
	}

	batchReq := &openfgav1.BatchCheckRequest{
		StoreId: storeID,
		Checks:  checks,
	}

	h.log.V(5).Info("calling BatchCheck", "storeID", storeID, "checkCount", len(checks))

	batchResp, err := h.fga.BatchCheck(ctx, batchReq)
	if err != nil {
		h.log.Error(err, "BatchCheck failed", "storeID", storeID)
		return nil, fmt.Errorf("batch check failed: %w", err)
	}

	for correlationID, singleResult := range batchResp.Result {
		if singleResult.GetError() != nil {
			h.log.Error(nil, "FGA check error", "id", correlationID, "error", singleResult.GetError().GetMessage())
			continue
		}
		results[correlationID] = singleResult.GetAllowed()
	}

	return h.buildResponse(results), nil
}

// buildChecks builds FGA BatchCheckItems from SubjectAccessReviews.
func (h *BatchHandler) buildChecks(sars []authorizationv1.SubjectAccessReview) ([]*openfgav1.BatchCheckItem, string, error) {
	checks := make([]*openfgav1.BatchCheckItem, 0, len(sars))
	var storeID string

	// errors are logged but not returned, as we want to process as many checks as possible
	// even if a few are invalid, the user will be able to interact with the portal
	for _, sar := range sars {
		clusterPath := sar.Spec.Extra[h.clusterPathKey][0]

		clusterName, ok := h.clusterCache.ClusterName(clusterPath)
		if !ok {
			h.log.Error(nil, "cluster not found in cache", "clusterPath", clusterPath)
			continue
		}

		clusterInfo, ok := h.clusterCache.Get(clusterName)
		if !ok {
			h.log.Error(nil, "cluster info not found in cache", "clusterName", clusterName)
			continue
		}

		// all items in the batch should have the same store ID
		// because all checks happens within single organization scope
		// if this changes, we should splitting the batch into multiple requests per store ID
		if storeID == "" {
			storeID = clusterInfo.StoreID
		} else if storeID != clusterInfo.StoreID {
			return nil, "", fmt.Errorf("%w: expected %s, got %s", ErrStoreIDMismatch, storeID, clusterInfo.StoreID)
		}

		// collect contextual tuples and check input for each SAR
		checkInput, err := contextual.BuildCheckInput(sar.Spec.ResourceAttributes, sar.Spec.User, clusterName.String(), clusterInfo)
		if err != nil {
			h.log.Error(err, "failed to build check input", "name", sar.Name)
			continue
		}

		fgaItem := &openfgav1.BatchCheckItem{
			CorrelationId: sar.Name,
			TupleKey: &openfgav1.CheckRequestTupleKey{
				Object:   checkInput.Object,
				Relation: checkInput.Relation,
				User:     checkInput.User,
			},
		}
		if len(checkInput.ContextualTuples) > 0 {
			fgaItem.ContextualTuples = &openfgav1.ContextualTupleKeys{
				TupleKeys: checkInput.ContextualTuples,
			}
		}
		checks = append(checks, fgaItem)
	}

	return checks, storeID, nil
}

// buildResponse converts the results map to BatchAuthzResult slice.
func (h *BatchHandler) buildResponse(results map[string]bool) []BatchAuthzResult {
	response := make([]BatchAuthzResult, 0, len(results))
	for id, allowed := range results {
		response = append(response, BatchAuthzResult{
			ID:      id,
			Allowed: allowed,
		})
	}
	return response
}

// writeError writes an error response.
func (h *BatchHandler) writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
