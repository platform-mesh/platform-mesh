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
	"context"
	"os"
	"time"

	"github.com/spf13/pflag"

	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/traces"
)

func SetConfigInContext(ctx context.Context, config any) context.Context {
	return context.WithValue(ctx, keys.ConfigCtxKey, config)
}

func LoadConfigFromContext(ctx context.Context) any {
	return ctx.Value(keys.ConfigCtxKey)
}

type ImageConfig struct {
	Name string
	Tag  string
}

type LogConfig struct {
	Level  string
	NoJson bool
}

type MetricsConfig struct {
	BindAddress string
	Secure      bool
}

type TracingConfig struct {
	Enabled   bool
	Collector traces.Config
}

type SentryConfig struct {
	Dsn string
}

type CommonServiceConfig struct {
	DebugLabelValue         string
	MaxConcurrentReconciles int
	Environment             string
	Region                  string
	Kubeconfig              string
	IsLocal                 bool

	Image ImageConfig

	Log LogConfig

	ShutdownTimeout        time.Duration
	Metrics                MetricsConfig
	Tracing                TracingConfig
	EnableHTTP2            bool
	HealthProbeBindAddress string

	LeaderElectionEnabled bool

	Sentry SentryConfig
}

func NewDefaultConfig() *CommonServiceConfig {

	config := &CommonServiceConfig{
		DebugLabelValue:         "",
		MaxConcurrentReconciles: 10,
		Environment:             "",
		Region:                  "local",
		Kubeconfig:              "",
		IsLocal:                 false,

		Image: ImageConfig{
			Name: "",
			Tag:  "",
		},

		Log: LogConfig{
			Level:  "info",
			NoJson: false,
		},

		ShutdownTimeout: time.Minute,

		Metrics: MetricsConfig{
			BindAddress: ":9090",
			Secure:      false,
		},

		Tracing: TracingConfig{
			Enabled:   false,
			Collector: traces.Config{},
		},

		EnableHTTP2:            true,
		HealthProbeBindAddress: ":8090",

		LeaderElectionEnabled: false,

		Sentry: SentryConfig{
			Dsn: os.Getenv("SENTRY_DSN"),
		},
	}

	return config
}

func (c *CommonServiceConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.DebugLabelValue, "debug-label-value", c.DebugLabelValue, "Set the debug label value")
	fs.IntVar(&c.MaxConcurrentReconciles, "max-concurrent-reconciles", c.MaxConcurrentReconciles, "Set the max concurrent reconciles")
	fs.StringVar(&c.Environment, "environment", c.Environment, "Set the environment of the service")
	fs.StringVar(&c.Region, "region", c.Region, "Set the region of the service, e.g. local, dev, staging, prod")
	fs.StringVar(&c.Kubeconfig, "kubeconfig", c.Kubeconfig, "Set the kubeconfig path")
	fs.BoolVar(&c.IsLocal, "is-local", c.IsLocal, "Flagging execution to be local")

	fs.StringVar(&c.Image.Name, "image-name", c.Image.Name, "Set the image name")
	fs.StringVar(&c.Image.Tag, "image-tag", c.Image.Tag, "Set the image tag")

	fs.StringVar(&c.Log.Level, "log-level", c.Log.Level, "Set the log level")
	fs.BoolVar(&c.Log.NoJson, "no-json", c.Log.NoJson, "Disable JSON logging")

	fs.DurationVar(&c.ShutdownTimeout, "shutdown-timeout", c.ShutdownTimeout, "Set the shutdown timeout as duration in seconds, e.g. 30s, 1m, 2h")
	fs.StringVar(&c.Metrics.BindAddress, "metrics-bind-address", c.Metrics.BindAddress, "Set the metrics bind address")
	fs.BoolVar(&c.Metrics.Secure, "metrics-secure", c.Metrics.Secure, "Set if metrics should be exposed via https")

	fs.BoolVar(&c.Tracing.Enabled, "tracing-enabled", c.Tracing.Enabled, "Enable tracing for the service")
	fs.StringVar(&c.Tracing.Collector.ServiceName, "tracing-config-service-name", c.Tracing.Collector.ServiceName, "Set the tracing service name used in traces")
	fs.StringVar(&c.Tracing.Collector.ServiceVersion, "tracing-config-service-version", c.Tracing.Collector.ServiceVersion, "Set the tracing service version used in traces")
	fs.StringVar(&c.Tracing.Collector.CollectorEndpoint, "tracing-config-collector-endpoint", c.Tracing.Collector.CollectorEndpoint, "Set the tracing collector endpoint used to send traces to the collector")

	fs.BoolVar(&c.EnableHTTP2, "enable-http2", c.EnableHTTP2, "Enable HTTP/2 for metrics and webhook servers")
	fs.StringVar(&c.HealthProbeBindAddress, "health-probe-bind-address", c.HealthProbeBindAddress, "Set the health probe bind address")

	fs.BoolVar(&c.LeaderElectionEnabled, "leader-elect", c.LeaderElectionEnabled, "Enable leader election for the controller manager")
}
