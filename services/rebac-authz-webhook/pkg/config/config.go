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
	"time"

	"github.com/spf13/pflag"
)

type WebhookConfig struct {
	CertDir                    string
	ClusterKey                 string
	AllowedNonResourcePrefixes []string

	// CacheMissMaxRetries is the maximum number of retries per key before stopping.
	CacheMissMaxRetries uint
	// CacheMissTTL is the duration after which retry count resets for a key.
	CacheMissTTL time.Duration
	// CacheMissCleanupInterval is the interval at which keys are checked for expiration.
	CacheMissCleanupInterval time.Duration
	// CacheMissRetryAfter is the delay before retrying on cache miss.
	CacheMissRetryAfter time.Duration
}

type Config struct {
	MetricsBindAddress     string
	HealthProbeBindAddress string
	OpenFGAAddr            string

	Webhook WebhookConfig

	APIExportEndpointSliceName string
}

func New() *Config {
	return &Config{
		MetricsBindAddress:     ":9090",
		HealthProbeBindAddress: ":8090",
		OpenFGAAddr:            "openfga.platform-mesh-system:8081",
		Webhook: WebhookConfig{
			CertDir:                    "config",
			ClusterKey:                 "authorization.kubernetes.io/cluster-name",
			AllowedNonResourcePrefixes: []string{"/api", "/openapi", "/version"},
			CacheMissMaxRetries:        1,
			CacheMissTTL:               5 * time.Minute,
			CacheMissCleanupInterval:   2 * time.Minute,
			CacheMissRetryAfter:        1 * time.Second,
		},

		APIExportEndpointSliceName: "core.platform-mesh.io",
	}
}

func (cfg *Config) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&cfg.MetricsBindAddress, "metrics-bind-address", cfg.MetricsBindAddress, "Set the metrics bind address")
	fs.StringVar(&cfg.HealthProbeBindAddress, "health-probe-bind-address", cfg.HealthProbeBindAddress, "Set the health probe bind address")
	fs.StringVar(&cfg.OpenFGAAddr, "openfga-addr", cfg.OpenFGAAddr, "Set the OpenFGA address")
	fs.StringVar(&cfg.Webhook.CertDir, "webhook-cert-dir", cfg.Webhook.CertDir, "Set the webhook certificate directory")
	fs.StringVar(&cfg.Webhook.ClusterKey, "webhook-cluster-key", cfg.Webhook.ClusterKey, "Set the webhook cluster key")
	fs.StringSliceVar(&cfg.Webhook.AllowedNonResourcePrefixes, "webhook-allowed-nonresource-prefixes", cfg.Webhook.AllowedNonResourcePrefixes, "Set the allowed non-resource prefixes for the webhook")
	fs.UintVar(&cfg.Webhook.CacheMissMaxRetries, "webhook-cache-miss-max-retries", cfg.Webhook.CacheMissMaxRetries, "Maximum number of retries per cluster on cache miss")
	fs.DurationVar(&cfg.Webhook.CacheMissTTL, "webhook-cache-miss-ttl", cfg.Webhook.CacheMissTTL, "Duration after which cache miss count resets for a cluster")
	fs.DurationVar(&cfg.Webhook.CacheMissCleanupInterval, "webhook-cache-miss-cleanup-interval", cfg.Webhook.CacheMissCleanupInterval, "Interval at which cache miss keys are checked for expiration")
	fs.DurationVar(&cfg.Webhook.CacheMissRetryAfter, "webhook-cache-miss-retry-after", cfg.Webhook.CacheMissRetryAfter, "Delay before retrying on cache miss")
	fs.StringVar(&cfg.APIExportEndpointSliceName, "kcp-api-export-endpoint-slice-name", cfg.APIExportEndpointSliceName, "Set the KCP API export endpoint slice name")
}
