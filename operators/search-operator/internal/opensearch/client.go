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

package opensearch

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/opensearch-project/opensearch-go/v4"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
	"github.com/opensearch-project/opensearch-go/v4/opensearchutil"
	"go.platform-mesh.io/golang-commons/logger"

	"go.platform-mesh.io/search-operator/internal/config"
	"go.platform-mesh.io/search-operator/internal/metrics"
)

// Client wraps the OpenSearch client with convenience methods
type Client struct {
	api *opensearchapi.Client
}

type Config struct {
	// URL is the OpenSearch server URL (e.g., https://localhost:9200)
	URL string
	// Username for basic auth
	Username string
	// Password for basic auth
	Password string
	// InsecureSkipVerify skips TLS certificate verification (for development)
	InsecureSkipVerify bool
}

// NewClient creates a new OpenSearch client
func NewClient(cfg Config) (*Client, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		},
	}

	client, err := opensearchapi.NewClient(
		opensearchapi.Config{
			Client: opensearch.Config{
				Transport: transport,
				Addresses: []string{cfg.URL},
				Username:  cfg.Username,
				Password:  cfg.Password,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenSearch client: %w", err)
	}

	return &Client{api: client}, nil
}

// NewClientFromEnv creates a new OpenSearch client using environment variables
// OPENSEARCH_URL, OPENSEARCH_USERNAME, OPENSEARCH_PASSWORD
func NewClientFromEnv(cfg *config.Config) (*Client, error) {
	appConfig, err := config.NewFromEnv()
	if err != nil {
		fmt.Printf("Error loading env file: %v\n", err)
		os.Exit(1)
	}
	url := os.Getenv("OPENSEARCH_URL")
	if url == "" {
		url = appConfig.OpenSearch.URL
	}
	fmt.Printf("url: %s", url)

	insecure := os.Getenv("OPENSEARCH_INSECURE") == "true"
	username := os.Getenv("OPENSEARCH_USERNAME")
	if username == "" {
		username = appConfig.OpenSearch.Username
	}
	password := os.Getenv("OPENSEARCH_PASSWORD")
	if password == "" {
		password = appConfig.OpenSearch.Password
	}

	return NewClient(Config{
		URL:                url,
		Username:           username,
		Password:           password,
		InsecureSkipVerify: insecure,
	})
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.api.Info(ctx, nil)

	return err
}

// IndexSettings contains index-level shard and replica settings.
type IndexSettings struct {
	NumberOfShards   int32
	NumberOfReplicas int32
}

// CreateIndex creates an index if it doesn't exist and applies initial settings.
func (c *Client) CreateIndex(ctx context.Context, indexName string, numberOfShards, numberOfReplicas int32, mapping string) (err error) {
	start := time.Now()
	defer func() {
		labelResult := "success"
		if err != nil {
			labelResult = "error"
		}
		metrics.OpenSearchOperationsTotal.WithLabelValues("create_index", labelResult).Inc()
		metrics.OpenSearchOperationDuration.WithLabelValues("create_index").Observe(time.Since(start).Seconds())
	}()
	log := logger.LoadLoggerFromContext(ctx)

	exists, err := c.IndexExists(ctx, indexName)
	if err != nil {
		return err
	}

	if exists {
		log.Debug().Str("index", indexName).Msg("index already exists")
		return nil
	}

	createBody := map[string]interface{}{
		"settings": map[string]interface{}{
			"index": map[string]interface{}{
				"number_of_shards":   numberOfShards,
				"number_of_replicas": numberOfReplicas,
				"knn":                true,
			},
		},
	}
	if mapping != "" {
		var mappingsPayload interface{}
		if err := json.Unmarshal([]byte(mapping), &mappingsPayload); err != nil {
			return fmt.Errorf("failed to parse index mapping payload: %w", err)
		}
		createBody["mappings"] = mappingsPayload
	}

	rawBody, err := json.Marshal(createBody)
	if err != nil {
		return fmt.Errorf("failed to marshal create index body: %w", err)
	}
	body := io.Reader(strings.NewReader(string(rawBody)))

	_, err = c.api.Indices.Create(
		ctx,
		opensearchapi.IndicesCreateReq{
			Index: indexName,
			Body:  body,
		},
	)
	if err != nil {
		var opensearchError *opensearch.StructError
		if errors.As(err, &opensearchError) {
			if opensearchError.Err.Type == "resource_already_exists_exception" {
				log.Debug().Str("index", indexName).Msg("index already exists (concurrent creation)")
				return nil
			}
		}
		return fmt.Errorf("failed to create index %s: %w", indexName, err)
	}

	log.Info().Str("index", indexName).Msg("created index")
	return nil
}

// GetIndexSettings returns current number_of_shards and number_of_replicas for an index.
func (c *Client) GetIndexSettings(ctx context.Context, indexName string) (IndexSettings, error) {
	resp, err := c.api.Indices.Settings.Get(ctx, &opensearchapi.SettingsGetReq{
		Indices: []string{indexName},
		Settings: []string{
			"index.number_of_shards",
			"index.number_of_replicas",
		},
	})
	if err != nil {
		return IndexSettings{}, fmt.Errorf("failed to get settings for index %s: %w", indexName, err)
	}
	if resp == nil {
		return IndexSettings{}, fmt.Errorf("failed to get settings for index %s: empty response", indexName)
	}

	indexEntry, ok := resp.Indices[indexName]
	if !ok {
		return IndexSettings{}, fmt.Errorf("settings for index %s not found in response", indexName)
	}

	var parsed struct {
		Index map[string]string `json:"index"`
	}
	if err := json.Unmarshal(indexEntry.Settings, &parsed); err != nil {
		return IndexSettings{}, fmt.Errorf("failed to decode settings for index %s: %w", indexName, err)
	}

	shardsStr, ok := parsed.Index["number_of_shards"]
	if !ok {
		return IndexSettings{}, fmt.Errorf("number_of_shards missing in settings for index %s", indexName)
	}
	replicasStr, ok := parsed.Index["number_of_replicas"]
	if !ok {
		return IndexSettings{}, fmt.Errorf("number_of_replicas missing in settings for index %s", indexName)
	}

	shardsValue, err := strconv.ParseInt(shardsStr, 10, 32)
	if err != nil {
		return IndexSettings{}, fmt.Errorf("invalid number_of_shards value %q for index %s: %w", shardsStr, indexName, err)
	}
	replicasValue, err := strconv.ParseInt(replicasStr, 10, 32)
	if err != nil {
		return IndexSettings{}, fmt.Errorf("invalid number_of_replicas value %q for index %s: %w", replicasStr, indexName, err)
	}

	return IndexSettings{
		NumberOfShards:   int32(shardsValue),
		NumberOfReplicas: int32(replicasValue),
	}, nil
}

// UpdateIndexReplicas updates number_of_replicas for an existing index.
func (c *Client) UpdateIndexReplicas(ctx context.Context, indexName string, numberOfReplicas int32) (err error) {
	start := time.Now()
	defer func() {
		labelResult := "success"
		if err != nil {
			labelResult = "error"
		}
		metrics.OpenSearchOperationsTotal.WithLabelValues("update_replicas", labelResult).Inc()
		metrics.OpenSearchOperationDuration.WithLabelValues("update_replicas").Observe(time.Since(start).Seconds())
	}()
	bodyJSON := fmt.Sprintf(`{"index":{"number_of_replicas":%d}}`, numberOfReplicas)
	_, err = c.api.Indices.Settings.Put(ctx, opensearchapi.SettingsPutReq{
		Indices: []string{indexName},
		Body:    strings.NewReader(bodyJSON),
	})
	if err != nil {
		return fmt.Errorf("failed to update number_of_replicas for index %s: %w", indexName, err)
	}

	return nil
}

// EnsureAliases ensures that all provided aliases exist for the given index.
func (c *Client) EnsureAliases(ctx context.Context, indexName string, aliases []string) error {
	log := logger.LoadLoggerFromContext(ctx)

	seen := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" || alias == indexName {
			continue
		}
		if _, exists := seen[alias]; exists {
			continue
		}
		seen[alias] = struct{}{}

		_, err := c.api.Indices.Alias.Put(ctx, opensearchapi.AliasPutReq{
			Indices: []string{indexName},
			Alias:   alias,
		})
		if err != nil {
			return fmt.Errorf("failed to ensure alias %q for index %q: %w", alias, indexName, err)
		}

		log.Debug().
			Str("index", indexName).
			Str("alias", alias).
			Msg("ensured alias for index")
	}

	return nil
}

// IndexExists checks if an index exists
func (c *Client) IndexExists(ctx context.Context, indexName string) (bool, error) {
	resp, err := c.api.Indices.Exists(ctx, opensearchapi.IndicesExistsReq{
		Indices: []string{indexName},
	})
	if err != nil {
		var opensearchError *opensearch.StructError
		if errors.As(err, &opensearchError) {
			if opensearchError.Err.Type == "index_not_found_exception" {
				return false, nil
			}
		}

		if resp.StatusCode == http.StatusNotFound {
			return false, nil
		}

		return false, fmt.Errorf("failed to check index existence: %w", err)
	}
	if resp == nil {
		return false, fmt.Errorf("failed to check index existence: empty response")
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return false, fmt.Errorf("failed to check index existence: status %s", resp.Status())
	}
	return true, nil
}

// DeleteIndex deletes an index
func (c *Client) DeleteIndex(ctx context.Context, indexName string) (err error) {
	start := time.Now()
	defer func() {
		labelResult := "success"
		if err != nil {
			labelResult = "error"
		}
		metrics.OpenSearchOperationsTotal.WithLabelValues("delete_index", labelResult).Inc()
		metrics.OpenSearchOperationDuration.WithLabelValues("delete_index").Observe(time.Since(start).Seconds())
	}()
	log := logger.LoadLoggerFromContext(ctx)

	exists, err := c.IndexExists(ctx, indexName)
	if err != nil {
		return err
	}

	if !exists {
		log.Debug().Str("index", indexName).Msg("index does not exist")
		return nil
	}

	_, err = c.api.Indices.Delete(ctx, opensearchapi.IndicesDeleteReq{
		Indices: []string{indexName},
	})
	if err != nil {
		return fmt.Errorf("failed to delete index %s: %w", indexName, err)
	}

	log.Info().Str("index", indexName).Msg("deleted index")
	return nil
}

// IndexDocument indexes a document
func (c *Client) IndexDocument(ctx context.Context, indexName, docID string, document interface{}) (err error) {
	start := time.Now()
	defer func() {
		labelResult := "success"
		if err != nil {
			labelResult = "error"
		}
		metrics.OpenSearchOperationsTotal.WithLabelValues("index_document", labelResult).Inc()
		metrics.OpenSearchOperationDuration.WithLabelValues("index_document").Observe(time.Since(start).Seconds())
	}()
	log := logger.LoadLoggerFromContext(ctx)

	resp, err := c.api.Index(
		ctx,
		opensearchapi.IndexReq{
			Index:      indexName,
			DocumentID: docID,
			Body:       opensearchutil.NewJSONReader(document),
			Params: opensearchapi.IndexParams{
				Refresh: "true", // Make document immediately searchable
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to index document %s in %s: %w", docID, indexName, err)
	}

	log.Debug().
		Str("index", resp.Index).
		Str("docID", resp.ID).
		Str("result", resp.Result).
		Msg("indexed document")

	return nil
}

// DeleteDocument deletes a document
func (c *Client) DeleteDocument(ctx context.Context, indexName, docID string) (err error) {
	start := time.Now()
	defer func() {
		labelResult := "success"
		if err != nil {
			labelResult = "error"
		}
		metrics.OpenSearchOperationsTotal.WithLabelValues("delete_document", labelResult).Inc()
		metrics.OpenSearchOperationDuration.WithLabelValues("delete_document").Observe(time.Since(start).Seconds())
	}()
	log := logger.LoadLoggerFromContext(ctx)

	resp, err := c.api.Document.Delete(ctx, opensearchapi.DocumentDeleteReq{
		Index:      indexName,
		DocumentID: docID,
	})
	if err != nil {
		return fmt.Errorf("failed to delete document %s from %s: %w", docID, indexName, err)
	}

	log.Debug().
		Str("index", resp.Index).
		Str("docID", docID).
		Str("result", resp.Result).
		Msg("deleted document")

	return nil
}
