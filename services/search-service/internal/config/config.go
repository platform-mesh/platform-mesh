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

package config

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

type OpenSearchConfig struct {
	URL      string
	Username string
	Password string
	Insecure bool
	Timeout  time.Duration
}

type OpenFGAConfig struct {
	GRPCAddr string
}

type SearchIndexConfig struct {
	WorkspacePath    string
	OrgWorkspacePath string
	Group            string
	Version          string
	Resource         string
}

type SearchConfig struct {
	DefaultLimit   int
	MaxLimit       int
	FetchBatchSize int
	MaxScannedHits int
}

type ServiceConfig struct {
	Port                int
	LocalDevelopmentOrg string
	OpenSearch          OpenSearchConfig
	OpenFGA             OpenFGAConfig
	SearchIndex         SearchIndexConfig
	Search              SearchConfig
}

func NewServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		Port:                8080,
		LocalDevelopmentOrg: localDevelopmentOrgFromEnv(),
		OpenSearch: OpenSearchConfig{
			URL:      "http://opensearch.platform-mesh-system.svc.cluster.local:9200",
			Username: os.Getenv("OPENSEARCH_USERNAME"),
			Password: os.Getenv("OPENSEARCH_PASSWORD"),
			Insecure: false,
			Timeout:  10 * time.Second,
		},
		OpenFGA: OpenFGAConfig{
			GRPCAddr: "openfga:8081",
		},
		SearchIndex: SearchIndexConfig{
			WorkspacePath:    "root:providers:search",
			OrgWorkspacePath: "root:orgs",
			Group:            "search.platform-mesh.io",
			Version:          "v1alpha1",
			Resource:         "searchindexes",
		},
		Search: SearchConfig{
			DefaultLimit:   20,
			MaxLimit:       100,
			FetchBatchSize: 100,
			MaxScannedHits: 1000,
		},
	}
}

func (c *ServiceConfig) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&c.Port, "port", c.Port, "Set the service port")
	fs.StringVar(&c.LocalDevelopmentOrg, "local-development-org", c.LocalDevelopmentOrg, "Organization to use when request host is localhost")

	fs.StringVar(&c.OpenSearch.URL, "opensearch-url", c.OpenSearch.URL, "Set OpenSearch URL")
	fs.StringVar(&c.OpenSearch.Username, "opensearch-username", c.OpenSearch.Username, "Set OpenSearch username")
	fs.StringVar(&c.OpenSearch.Password, "opensearch-password", c.OpenSearch.Password, "Set OpenSearch password")
	fs.BoolVar(&c.OpenSearch.Insecure, "opensearch-insecure", c.OpenSearch.Insecure, "Allow insecure TLS for OpenSearch")
	fs.DurationVar(&c.OpenSearch.Timeout, "opensearch-timeout", c.OpenSearch.Timeout, "Set OpenSearch HTTP timeout")

	fs.StringVar(&c.OpenFGA.GRPCAddr, "openfga-grpc-addr", c.OpenFGA.GRPCAddr, "Set OpenFGA gRPC address")

	fs.StringVar(&c.SearchIndex.WorkspacePath, "searchindex-workspace-path", c.SearchIndex.WorkspacePath, "Workspace path for SearchIndex resources")
	fs.StringVar(&c.SearchIndex.OrgWorkspacePath, "searchindex-org-workspace-path", c.SearchIndex.OrgWorkspacePath, "Workspace path for organization workspaces")
	fs.StringVar(&c.SearchIndex.Group, "searchindex-group", c.SearchIndex.Group, "SearchIndex API group")
	fs.StringVar(&c.SearchIndex.Version, "searchindex-version", c.SearchIndex.Version, "SearchIndex API version")
	fs.StringVar(&c.SearchIndex.Resource, "searchindex-resource", c.SearchIndex.Resource, "SearchIndex API resource plural")

	fs.IntVar(&c.Search.DefaultLimit, "search-default-limit", c.Search.DefaultLimit, "Default search result limit")
	fs.IntVar(&c.Search.MaxLimit, "search-max-limit", c.Search.MaxLimit, "Maximum search result limit")
	fs.IntVar(&c.Search.FetchBatchSize, "search-fetch-batch-size", c.Search.FetchBatchSize, "Batch size for OpenSearch fetches")
	fs.IntVar(&c.Search.MaxScannedHits, "search-max-scanned-hits", c.Search.MaxScannedHits, "Maximum raw hits scanned per request")
}

func localDevelopmentOrgFromEnv() string {
	v := strings.TrimSpace(os.Getenv("SEARCH_LOCAL_ORG"))
	if v == "" {
		return "local"
	}
	return v
}
