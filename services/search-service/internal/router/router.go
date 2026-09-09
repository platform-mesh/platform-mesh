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

package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"go.platform-mesh.io/golang-commons/logger"
	appcontext "go.platform-mesh.io/search-service/internal/context"
	"go.platform-mesh.io/search-service/internal/service/search"
)

var fgaRelationPattern = regexp.MustCompile(`^[^:#@\s]{1,50}$`)

type SearchService interface {
	Search(ctx context.Context, req search.SearchRequest) (search.SearchResponse, error)
	ListResources(ctx context.Context, req search.SearchResourcesRequest) (search.SearchResourcesResponse, error)
	FilterValues(ctx context.Context, req search.FilterValuesRequest) (search.FilterValuesResponse, error)
}

func CreateRouter(svc SearchService, mws []func(http.Handler) http.Handler) *chi.Mux {
	router := chi.NewRouter()

	router.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	router.With(mws...).Get("/rest/v1/search", func(w http.ResponseWriter, r *http.Request) {
		rc, err := appcontext.GetRequestContext(r.Context())
		if err != nil {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		q := strings.TrimSpace(r.URL.Query().Get("q"))
		limit, err := parseOptionalLimit(r.URL.Query().Get("limit"))
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		page, err := parseOptionalPage(r.URL.Query().Get("page"))
		if err != nil {
			http.Error(w, "invalid page", http.StatusBadRequest)
			return
		}

		resource := strings.TrimSpace(r.URL.Query().Get("resource"))

		filters, fgaRole, err := parseSearchFilters(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resources := resolveResources(
			resource,
			r.URL.Query().Get("resources"),
			len(filters) != 0 || fgaRole != "",
		)
		if resources == nil {
			http.Error(w, "Filtering is not supported for searching across all resources.", http.StatusBadRequest)
			return
		}

		resultSet := make(map[string]search.SearchResponse, len(resources))
		for _, res := range resources {
			resp, err := svc.Search(r.Context(), search.SearchRequest{
				Organization: rc.Organization,
				User:         rc.User,
				Query:        q,
				Mode:         strings.TrimSpace(r.URL.Query().Get("mode")),
				Resource:     res,
				Filters:      filters,
				FGARole:      fgaRole,
				Limit:        limit,
				Page:         page,
				Cursor:       strings.TrimSpace(r.URL.Query().Get("cursor")),
			})
			if err != nil {
				handleError(w, r, rc, err)
				return
			}
			resultSet[res] = resp
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if len(resultSet) == 1 {
			var result search.SearchResponse
			for _, v := range resultSet {
				result = v
			}
			_ = json.NewEncoder(w).Encode(result)
		} else {
			_ = json.NewEncoder(w).Encode(resultSet)
		}
	})

	router.With(mws...).Get("/rest/v1/search/resources", func(w http.ResponseWriter, r *http.Request) {
		rc, err := appcontext.GetRequestContext(r.Context())
		if err != nil {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		resp, err := svc.ListResources(r.Context(), search.SearchResourcesRequest{
			Organization: rc.Organization,
		})
		if err != nil {
			handleError(w, r, rc, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	router.With(mws...).Get("/rest/v1/search/filter-values", func(w http.ResponseWriter, r *http.Request) {
		rc, err := appcontext.GetRequestContext(r.Context())
		if err != nil {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		limit, err := parseOptionalLimit(r.URL.Query().Get("limit"))
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}

		filters, err := parseFilters(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp, err := svc.FilterValues(r.Context(), search.FilterValuesRequest{
			Organization: rc.Organization,
			User:         rc.User,
			Resource:     strings.TrimSpace(r.URL.Query().Get("resource")),
			Field:        strings.TrimSpace(r.URL.Query().Get("field")),
			Query:        strings.TrimSpace(r.URL.Query().Get("q")),
			Filters:      filters,
			Limit:        limit,
		})
		if err != nil {
			handleError(w, r, rc, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	return router
}

// resolveResources decides which resources a search should run against:
//   - a single "resource" param wins,
//   - otherwise a comma-separated "resources" param selects a subset,
//   - otherwise a single empty resource lets the service search everything.
func resolveResources(resource, resourcesParam string, hasFilters bool) []string {
	if resource != "" {
		return []string{resource}
	}

	if resources := parseResources(resourcesParam); len(resources) > 0 {
		return resources
	}

	if hasFilters {
		return nil
	}

	return []string{""}
}

func parseResources(raw string) []string {
	var resources []string
	for _, res := range strings.Split(raw, ",") {
		if res = strings.TrimSpace(res); res != "" {
			resources = append(resources, res)
		}
	}
	return resources
}

func parseOptionalLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}

func parseOptionalPage(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}

	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 0, fmt.Errorf("page must be a positive integer")
	}
	return page, nil
}

func parseFilters(values map[string][]string) (map[string][]string, error) {
	filters := make(map[string][]string)
	for key, entries := range values {
		if !strings.HasPrefix(key, "filter.") {
			continue
		}

		field := strings.TrimSpace(strings.TrimPrefix(key, "filter."))
		if field == "" {
			return nil, fmt.Errorf("invalid filter field")
		}

		for _, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			filters[field] = append(filters[field], entry)
		}
	}

	if len(filters) == 0 {
		return nil, nil
	}
	return filters, nil
}

func parseSearchFilters(values map[string][]string) (map[string][]string, string, error) {
	filters, err := parseFilters(values)
	if err != nil {
		return nil, "", err
	}

	entries, ok := values["filter.fga_role"]
	if !ok {
		return filters, "", nil
	}
	if len(entries) != 1 {
		return nil, "", fmt.Errorf("filter.fga_role must have exactly one value")
	}

	fgaRole := strings.TrimSpace(entries[0])
	if fgaRole == "" {
		return nil, "", fmt.Errorf("filter.fga_role must not be empty")
	}
	if !fgaRelationPattern.MatchString(fgaRole) {
		return nil, "", fmt.Errorf("filter.fga_role has an invalid format")
	}

	delete(filters, "fga_role")
	if len(filters) == 0 {
		filters = nil
	}

	return filters, fgaRole, nil
}

func handleError(w http.ResponseWriter, r *http.Request, rc appcontext.RequestContext, err error) {
	log := logger.LoadLoggerFromContext(r.Context())
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, search.ErrInvalidRequest), errors.Is(err, search.ErrInvalidCursor):
		status = http.StatusBadRequest
		http.Error(w, err.Error(), status)
	case errors.Is(err, search.ErrUnauthorized):
		status = http.StatusUnauthorized
		http.Error(w, http.StatusText(status), status)
	case errors.Is(err, search.ErrForbidden):
		status = http.StatusForbidden
		http.Error(w, http.StatusText(status), status)
	default:
		http.Error(w, http.StatusText(status), status)
	}
	log.Error().
		Err(err).
		Str("path", r.URL.Path).
		Str("organization", rc.Organization).
		Int("statusCode", status).
		Msg("search request failed")
}
