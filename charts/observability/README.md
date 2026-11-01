# observability

OpenTelemetry-based observability stack for platform-mesh

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| createNamespace | bool | `false` | Create the namespace if it doesn't exist (usually handled externally) |
| httproutes | object | `{"gatewayName":"","gatewayNamespace":"","otelCollector":{"enabled":false,"filters":[],"hostnames":[],"pathPrefix":"/otel","port":8888},"prometheus":{"enabled":false,"filters":[],"hostnames":[],"pathPrefix":"/prometheus"}}` | ---------------------------------------------------------------------------- |
| httproutes.gatewayName | string | `""` | Name of the Gateway to attach routes to |
| httproutes.gatewayNamespace | string | `""` | Namespace of the Gateway (optional, defaults to same namespace) |
| httproutes.otelCollector | object | `{"enabled":false,"filters":[],"hostnames":[],"pathPrefix":"/otel","port":8888}` | OpenTelemetry Collector HTTPRoute |
| httproutes.otelCollector.enabled | bool | `false` | Enable HTTPRoute for OTel Collector |
| httproutes.otelCollector.filters | list | `[]` | Optional filters |
| httproutes.otelCollector.hostnames | list | `[]` | Hostnames for the OTel Collector |
| httproutes.otelCollector.pathPrefix | string | `"/otel"` | Path prefix for OTel Collector |
| httproutes.otelCollector.port | int | `8888` | Port to expose (metrics port 8888 or receiver ports like 4318 for OTLP HTTP) |
| httproutes.prometheus | object | `{"enabled":false,"filters":[],"hostnames":[],"pathPrefix":"/prometheus"}` | Prometheus UI HTTPRoute |
| httproutes.prometheus.enabled | bool | `false` | Enable HTTPRoute for Prometheus |
| httproutes.prometheus.filters | list | `[]` | Optional filters (e.g., URLRewrite for path stripping) |
| httproutes.prometheus.hostnames | list | `[]` | Hostnames for the Prometheus UI |
| httproutes.prometheus.pathPrefix | string | `"/prometheus"` | Path prefix for Prometheus UI |
| otelCollector | object | `{"config":{"batchTimeout":"10s"},"image":{"digest":"","registry":"ghcr.io","repository":"open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib","tag":"0.153.0"},"mode":"statefulset","name":"otel-gateway","replicas":1}` | ---------------------------------------------------------------------------- |
| otelCollector.config.batchTimeout | string | `"10s"` | Batch processor timeout before sending metrics |
| otelCollector.image | object | `{"digest":"","registry":"ghcr.io","repository":"open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib","tag":"0.153.0"}` | Collector image (use contrib for prometheusremotewrite exporter) |
| otelCollector.image.digest | string | `""` | Collector image digest (when set, appended to tag as repo:tag@digest) |
| otelCollector.image.registry | string | `"ghcr.io"` | Collector image registry |
| otelCollector.image.repository | string | `"open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib"` | Collector image repository (without registry) |
| otelCollector.image.tag | string | `"0.153.0"` | Collector image tag |
| otelCollector.mode | string | `"statefulset"` | Deployment mode (statefulset required for Target Allocator) |
| otelCollector.name | string | `"otel-gateway"` | Name of the OpenTelemetryCollector CR |
| otelCollector.replicas | int | `1` | Number of collector replicas |
| prometheus | object | `{"alertmanager":{"enabled":false},"enabled":true,"external":{"url":""},"kube-state-metrics":{"enabled":false},"prometheus-node-exporter":{"enabled":false},"prometheus-pushgateway":{"enabled":false},"server":{"extraFlags":["web.enable-remote-write-receiver","web.enable-lifecycle"],"persistentVolume":{"enabled":false},"service":{"type":"ClusterIP"}}}` | ---------------------------------------------------------------------------- |
| prometheus.alertmanager | object | `{"enabled":false}` | Disable components not needed for this POC |
| prometheus.enabled | bool | `true` | Enable Prometheus deployment |
| prometheus.external.url | string | `""` | Prometheus-compatible remote-write endpoint. When set, the OTel Collector sends metrics here instead of to the bundled Prometheus. Set prometheus.enabled to false separately to disable the bundled deployment. |
| prometheus.server.extraFlags[0] | string | `"web.enable-remote-write-receiver"` | Enable remote write receiver (required for OTel Collector to push metrics) |
| prometheus.server.extraFlags[1] | string | `"web.enable-lifecycle"` | Enable lifecycle API (required for config reloader sidecar) |
| prometheus.server.persistentVolume.enabled | bool | `false` | Disable persistence for local development |
| serviceMonitors | object | `{"accountOperator":{"enabled":true,"labels":{"app":"account-operator"},"namespace":"platform-mesh-system","path":"/metrics","port":"metrics"},"kcp":{"certExtractor":{"image":{"digest":"sha256:a67b11e95e953f550f020a41970185ccc5f83d78b86b8c575d02c904aa0f9cd7","registry":"docker.io","repository":"bitnami/kubectl","tag":"latest"}},"enabled":true,"kubeconfig":{"groups":["system:monitoring"],"name":"metrics-viewer","secretName":"kubeconfig-metrics-viewer","username":"metrics-viewer","validity":"8766h"},"labels":{"app.kubernetes.io/component":"rootshard","app.kubernetes.io/name":"kcp"},"namespace":"platform-mesh-system","path":"/clusters/root/metrics","port":"https","scheme":"https","tlsInsecureSkipVerify":true,"tlsSecretName":"kcp-metrics-client-cert"},"openfga":{"enabled":true,"labels":{"app.kubernetes.io/name":"openfga"},"namespace":"platform-mesh-system","path":"/metrics","port":"metrics"},"securityOperator":{"enabled":true,"labels":{"app":"security-operator"},"namespace":"platform-mesh-system","path":"/metrics","port":"metrics"}}` | ---------------------------------------------------------------------------- |
| serviceMonitors.accountOperator | object | `{"enabled":true,"labels":{"app":"account-operator"},"namespace":"platform-mesh-system","path":"/metrics","port":"metrics"}` | account-operator metrics scraping |
| serviceMonitors.accountOperator.enabled | bool | `true` | Enable scraping account-operator |
| serviceMonitors.accountOperator.labels | object | `{"app":"account-operator"}` | Labels to select account-operator metrics service |
| serviceMonitors.accountOperator.namespace | string | `"platform-mesh-system"` | Namespace where account-operator runs |
| serviceMonitors.accountOperator.path | string | `"/metrics"` | Path to metrics endpoint |
| serviceMonitors.accountOperator.port | string | `"metrics"` | Name of the port exposing metrics |
| serviceMonitors.kcp | object | `{"certExtractor":{"image":{"digest":"sha256:a67b11e95e953f550f020a41970185ccc5f83d78b86b8c575d02c904aa0f9cd7","registry":"docker.io","repository":"bitnami/kubectl","tag":"latest"}},"enabled":true,"kubeconfig":{"groups":["system:monitoring"],"name":"metrics-viewer","secretName":"kubeconfig-metrics-viewer","username":"metrics-viewer","validity":"8766h"},"labels":{"app.kubernetes.io/component":"rootshard","app.kubernetes.io/name":"kcp"},"namespace":"platform-mesh-system","path":"/clusters/root/metrics","port":"https","scheme":"https","tlsInsecureSkipVerify":true,"tlsSecretName":"kcp-metrics-client-cert"}` | kcp (root shard) metrics scraping Requires client certificate authentication |
| serviceMonitors.kcp.certExtractor | object | `{"image":{"digest":"sha256:a67b11e95e953f550f020a41970185ccc5f83d78b86b8c575d02c904aa0f9cd7","registry":"docker.io","repository":"bitnami/kubectl","tag":"latest"}}` | cert-extractor Job image (kubectl); localizable for air-gap |
| serviceMonitors.kcp.certExtractor.image.digest | string | `"sha256:a67b11e95e953f550f020a41970185ccc5f83d78b86b8c575d02c904aa0f9cd7"` | cert-extractor job image digest (pins the mutable :latest tag) |
| serviceMonitors.kcp.certExtractor.image.registry | string | `"docker.io"` | cert-extractor job image registry |
| serviceMonitors.kcp.certExtractor.image.repository | string | `"bitnami/kubectl"` | cert-extractor job image repository (without registry) |
| serviceMonitors.kcp.certExtractor.image.tag | string | `"latest"` | cert-extractor job image tag (cosmetic; digest pins the image) |
| serviceMonitors.kcp.enabled | bool | `true` | Enable scraping kcp |
| serviceMonitors.kcp.kubeconfig | object | `{"groups":["system:monitoring"],"name":"metrics-viewer","secretName":"kubeconfig-metrics-viewer","username":"metrics-viewer","validity":"8766h"}` | Kubeconfig configuration for kcp authentication The kcp-operator creates a kubeconfig with client certificates |
| serviceMonitors.kcp.kubeconfig.groups | list | `["system:monitoring"]` | Groups embedded in the client certificate system:monitoring grants read access to /clusters/root/metrics endpoints |
| serviceMonitors.kcp.kubeconfig.name | string | `"metrics-viewer"` | Name of the Kubeconfig CR to create |
| serviceMonitors.kcp.kubeconfig.secretName | string | `"kubeconfig-metrics-viewer"` | Name of the secret where kcp-operator stores the kubeconfig |
| serviceMonitors.kcp.kubeconfig.username | string | `"metrics-viewer"` | Username embedded in the client certificate |
| serviceMonitors.kcp.kubeconfig.validity | string | `"8766h"` | Validity period of the client certificate |
| serviceMonitors.kcp.labels | object | `{"app.kubernetes.io/component":"rootshard","app.kubernetes.io/name":"kcp"}` | Labels to select kcp service |
| serviceMonitors.kcp.namespace | string | `"platform-mesh-system"` | Namespace where kcp runs |
| serviceMonitors.kcp.path | string | `"/clusters/root/metrics"` | Path to metrics endpoint (as of kcp 0.32, /metrics and /clusters/root/metrics return the exact same data, but /clusters/root/metrics requires fewer permissions). |
| serviceMonitors.kcp.port | string | `"https"` | Name of the port exposing metrics (same as API port) |
| serviceMonitors.kcp.scheme | string | `"https"` | Use HTTPS scheme |
| serviceMonitors.kcp.tlsInsecureSkipVerify | bool | `true` | Skip TLS certificate verification; unless you know what you're doing, this should be left to true, since OTel Collector will connect to each Pod in the target Service using its IP, which is never part of the certificate's SANs. |
| serviceMonitors.kcp.tlsSecretName | string | `"kcp-metrics-client-cert"` | Name of the TLS secret containing client cert/key for authentication This secret is created by the kcp-metrics-cert-job from the kubeconfig |
| serviceMonitors.openfga | object | `{"enabled":true,"labels":{"app.kubernetes.io/name":"openfga"},"namespace":"platform-mesh-system","path":"/metrics","port":"metrics"}` | OpenFGA metrics scraping |
| serviceMonitors.openfga.enabled | bool | `true` | Enable scraping OpenFGA |
| serviceMonitors.openfga.labels | object | `{"app.kubernetes.io/name":"openfga"}` | Labels to select OpenFGA service |
| serviceMonitors.openfga.namespace | string | `"platform-mesh-system"` | Namespace where OpenFGA runs |
| serviceMonitors.openfga.path | string | `"/metrics"` | Path to metrics endpoint |
| serviceMonitors.openfga.port | string | `"metrics"` | Name of the port exposing metrics |
| serviceMonitors.securityOperator | object | `{"enabled":true,"labels":{"app":"security-operator"},"namespace":"platform-mesh-system","path":"/metrics","port":"metrics"}` | security-operator metrics scraping |
| serviceMonitors.securityOperator.enabled | bool | `true` | Enable scraping security-operator |
| serviceMonitors.securityOperator.labels | object | `{"app":"security-operator"}` | Labels to select security-operator metrics service |
| serviceMonitors.securityOperator.namespace | string | `"platform-mesh-system"` | Namespace where security-operator runs |
| serviceMonitors.securityOperator.path | string | `"/metrics"` | Path to metrics endpoint |
| serviceMonitors.securityOperator.port | string | `"metrics"` | Name of the port exposing metrics |

## Overriding Values

The values in the `defaults:` section can be reused from other charts by using the lookup function "common.getKeyValue". It implements lookup on three levels:

1. Looks for `keyOverride` in the chart's values.yaml
2. Looks for `global.key` in the chart's or parent chart's values.yaml
3. Uses the `key` in the chart's values.yaml
4. Uses the `common.defaults.key` value from the table below.

1 has precedence over 2 over 3 over 4 respectively. This approach allows for individual charts to have minimal configuration, while still being able to override parameters locally.

Example
```
1) .Values.deployment.resources.limits.memoryOverride = 4096MB
2) .Values.global.deployment.resources.limits.memory = 2048MB
3) .Values.deployment.resources.limits.memory = 1024MB
4) .Values.common.defaults.deployment.resources.limits.memory = default 512MB
```
# observability

![Version: 0.0.0](https://img.shields.io/badge/Version-0.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.0.0](https://img.shields.io/badge/AppVersion-0.0.0-informational?style=flat-square)

OpenTelemetry-based observability stack for platform-mesh

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| file://../common | common | 0.0.0 |
| https://prometheus-community.github.io/helm-charts | prometheus | 29.19.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| createNamespace | bool | `false` | Create the namespace if it doesn't exist (usually handled externally) |
| httproutes | object | `{"gatewayName":"","gatewayNamespace":"","otelCollector":{"enabled":false,"filters":[],"hostnames":[],"pathPrefix":"/otel","port":8888},"prometheus":{"enabled":false,"filters":[],"hostnames":[],"pathPrefix":"/prometheus"}}` | ---------------------------------------------------------------------------- |
| httproutes.gatewayName | string | `""` | Name of the Gateway to attach routes to |
| httproutes.gatewayNamespace | string | `""` | Namespace of the Gateway (optional, defaults to same namespace) |
| httproutes.otelCollector | object | `{"enabled":false,"filters":[],"hostnames":[],"pathPrefix":"/otel","port":8888}` | OpenTelemetry Collector HTTPRoute |
| httproutes.otelCollector.enabled | bool | `false` | Enable HTTPRoute for OTel Collector |
| httproutes.otelCollector.filters | list | `[]` | Optional filters |
| httproutes.otelCollector.hostnames | list | `[]` | Hostnames for the OTel Collector |
| httproutes.otelCollector.pathPrefix | string | `"/otel"` | Path prefix for OTel Collector |
| httproutes.otelCollector.port | int | `8888` | Port to expose (metrics port 8888 or receiver ports like 4318 for OTLP HTTP) |
| httproutes.prometheus | object | `{"enabled":false,"filters":[],"hostnames":[],"pathPrefix":"/prometheus"}` | Prometheus UI HTTPRoute |
| httproutes.prometheus.enabled | bool | `false` | Enable HTTPRoute for Prometheus |
| httproutes.prometheus.filters | list | `[]` | Optional filters (e.g., URLRewrite for path stripping) |
| httproutes.prometheus.hostnames | list | `[]` | Hostnames for the Prometheus UI |
| httproutes.prometheus.pathPrefix | string | `"/prometheus"` | Path prefix for Prometheus UI |
| otelCollector | object | `{"config":{"batchTimeout":"10s"},"image":{"digest":"","registry":"ghcr.io","repository":"open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib","tag":"0.153.0"},"mode":"statefulset","name":"otel-gateway","replicas":1}` | ---------------------------------------------------------------------------- |
| otelCollector.config.batchTimeout | string | `"10s"` | Batch processor timeout before sending metrics |
| otelCollector.image | object | `{"digest":"","registry":"ghcr.io","repository":"open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib","tag":"0.153.0"}` | Collector image (use contrib for prometheusremotewrite exporter) |
| otelCollector.image.digest | string | `""` | Collector image digest (when set, appended to tag as repo:tag@digest) |
| otelCollector.image.registry | string | `"ghcr.io"` | Collector image registry |
| otelCollector.image.repository | string | `"open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib"` | Collector image repository (without registry) |
| otelCollector.image.tag | string | `"0.153.0"` | Collector image tag |
| otelCollector.mode | string | `"statefulset"` | Deployment mode (statefulset required for Target Allocator) |
| otelCollector.name | string | `"otel-gateway"` | Name of the OpenTelemetryCollector CR |
| otelCollector.replicas | int | `1` | Number of collector replicas |
| prometheus | object | `{"alertmanager":{"enabled":false},"enabled":true,"external":{"url":""},"kube-state-metrics":{"enabled":false},"prometheus-node-exporter":{"enabled":false},"prometheus-pushgateway":{"enabled":false},"server":{"extraFlags":["web.enable-remote-write-receiver","web.enable-lifecycle"],"persistentVolume":{"enabled":false},"service":{"type":"ClusterIP"}}}` | ---------------------------------------------------------------------------- |
| prometheus.alertmanager | object | `{"enabled":false}` | Disable components not needed for this POC |
| prometheus.enabled | bool | `true` | Enable Prometheus deployment |
| prometheus.external.url | string | `""` | Prometheus-compatible remote-write endpoint. When set, the OTel Collector sends metrics here instead of to the bundled Prometheus. Set prometheus.enabled to false separately to disable the bundled deployment. |
| prometheus.server.extraFlags[0] | string | `"web.enable-remote-write-receiver"` | Enable remote write receiver (required for OTel Collector to push metrics) |
| prometheus.server.extraFlags[1] | string | `"web.enable-lifecycle"` | Enable lifecycle API (required for config reloader sidecar) |
| prometheus.server.persistentVolume.enabled | bool | `false` | Disable persistence for local development |
| serviceMonitors | object | `{"accountOperator":{"enabled":true,"labels":{"app":"account-operator"},"namespace":"platform-mesh-system","path":"/metrics","port":"metrics"},"kcp":{"certExtractor":{"image":{"digest":"sha256:a67b11e95e953f550f020a41970185ccc5f83d78b86b8c575d02c904aa0f9cd7","registry":"docker.io","repository":"bitnami/kubectl","tag":"latest"}},"enabled":true,"kubeconfig":{"groups":["system:monitoring"],"name":"metrics-viewer","secretName":"kubeconfig-metrics-viewer","username":"metrics-viewer","validity":"8766h"},"labels":{"app.kubernetes.io/component":"rootshard","app.kubernetes.io/name":"kcp"},"namespace":"platform-mesh-system","path":"/clusters/root/metrics","port":"https","scheme":"https","tlsInsecureSkipVerify":true,"tlsSecretName":"kcp-metrics-client-cert"},"openfga":{"enabled":true,"labels":{"app.kubernetes.io/name":"openfga"},"namespace":"platform-mesh-system","path":"/metrics","port":"metrics"},"securityOperator":{"enabled":true,"labels":{"app":"security-operator"},"namespace":"platform-mesh-system","path":"/metrics","port":"metrics"}}` | ---------------------------------------------------------------------------- |
| serviceMonitors.accountOperator | object | `{"enabled":true,"labels":{"app":"account-operator"},"namespace":"platform-mesh-system","path":"/metrics","port":"metrics"}` | account-operator metrics scraping |
| serviceMonitors.accountOperator.enabled | bool | `true` | Enable scraping account-operator |
| serviceMonitors.accountOperator.labels | object | `{"app":"account-operator"}` | Labels to select account-operator metrics service |
| serviceMonitors.accountOperator.namespace | string | `"platform-mesh-system"` | Namespace where account-operator runs |
| serviceMonitors.accountOperator.path | string | `"/metrics"` | Path to metrics endpoint |
| serviceMonitors.accountOperator.port | string | `"metrics"` | Name of the port exposing metrics |
| serviceMonitors.kcp | object | `{"certExtractor":{"image":{"digest":"sha256:a67b11e95e953f550f020a41970185ccc5f83d78b86b8c575d02c904aa0f9cd7","registry":"docker.io","repository":"bitnami/kubectl","tag":"latest"}},"enabled":true,"kubeconfig":{"groups":["system:monitoring"],"name":"metrics-viewer","secretName":"kubeconfig-metrics-viewer","username":"metrics-viewer","validity":"8766h"},"labels":{"app.kubernetes.io/component":"rootshard","app.kubernetes.io/name":"kcp"},"namespace":"platform-mesh-system","path":"/clusters/root/metrics","port":"https","scheme":"https","tlsInsecureSkipVerify":true,"tlsSecretName":"kcp-metrics-client-cert"}` | kcp (root shard) metrics scraping Requires client certificate authentication |
| serviceMonitors.kcp.certExtractor | object | `{"image":{"digest":"sha256:a67b11e95e953f550f020a41970185ccc5f83d78b86b8c575d02c904aa0f9cd7","registry":"docker.io","repository":"bitnami/kubectl","tag":"latest"}}` | cert-extractor Job image (kubectl); localizable for air-gap |
| serviceMonitors.kcp.certExtractor.image.digest | string | `"sha256:a67b11e95e953f550f020a41970185ccc5f83d78b86b8c575d02c904aa0f9cd7"` | cert-extractor job image digest (pins the mutable :latest tag) |
| serviceMonitors.kcp.certExtractor.image.registry | string | `"docker.io"` | cert-extractor job image registry |
| serviceMonitors.kcp.certExtractor.image.repository | string | `"bitnami/kubectl"` | cert-extractor job image repository (without registry) |
| serviceMonitors.kcp.certExtractor.image.tag | string | `"latest"` | cert-extractor job image tag (cosmetic; digest pins the image) |
| serviceMonitors.kcp.enabled | bool | `true` | Enable scraping kcp |
| serviceMonitors.kcp.kubeconfig | object | `{"groups":["system:monitoring"],"name":"metrics-viewer","secretName":"kubeconfig-metrics-viewer","username":"metrics-viewer","validity":"8766h"}` | Kubeconfig configuration for kcp authentication The kcp-operator creates a kubeconfig with client certificates |
| serviceMonitors.kcp.kubeconfig.groups | list | `["system:monitoring"]` | Groups embedded in the client certificate system:monitoring grants read access to /clusters/root/metrics endpoints |
| serviceMonitors.kcp.kubeconfig.name | string | `"metrics-viewer"` | Name of the Kubeconfig CR to create |
| serviceMonitors.kcp.kubeconfig.secretName | string | `"kubeconfig-metrics-viewer"` | Name of the secret where kcp-operator stores the kubeconfig |
| serviceMonitors.kcp.kubeconfig.username | string | `"metrics-viewer"` | Username embedded in the client certificate |
| serviceMonitors.kcp.kubeconfig.validity | string | `"8766h"` | Validity period of the client certificate |
| serviceMonitors.kcp.labels | object | `{"app.kubernetes.io/component":"rootshard","app.kubernetes.io/name":"kcp"}` | Labels to select kcp service |
| serviceMonitors.kcp.namespace | string | `"platform-mesh-system"` | Namespace where kcp runs |
| serviceMonitors.kcp.path | string | `"/clusters/root/metrics"` | Path to metrics endpoint (as of kcp 0.32, /metrics and /clusters/root/metrics return the exact same data, but /clusters/root/metrics requires fewer permissions). |
| serviceMonitors.kcp.port | string | `"https"` | Name of the port exposing metrics (same as API port) |
| serviceMonitors.kcp.scheme | string | `"https"` | Use HTTPS scheme |
| serviceMonitors.kcp.tlsInsecureSkipVerify | bool | `true` | Skip TLS certificate verification; unless you know what you're doing, this should be left to true, since OTel Collector will connect to each Pod in the target Service using its IP, which is never part of the certificate's SANs. |
| serviceMonitors.kcp.tlsSecretName | string | `"kcp-metrics-client-cert"` | Name of the TLS secret containing client cert/key for authentication This secret is created by the kcp-metrics-cert-job from the kubeconfig |
| serviceMonitors.openfga | object | `{"enabled":true,"labels":{"app.kubernetes.io/name":"openfga"},"namespace":"platform-mesh-system","path":"/metrics","port":"metrics"}` | OpenFGA metrics scraping |
| serviceMonitors.openfga.enabled | bool | `true` | Enable scraping OpenFGA |
| serviceMonitors.openfga.labels | object | `{"app.kubernetes.io/name":"openfga"}` | Labels to select OpenFGA service |
| serviceMonitors.openfga.namespace | string | `"platform-mesh-system"` | Namespace where OpenFGA runs |
| serviceMonitors.openfga.path | string | `"/metrics"` | Path to metrics endpoint |
| serviceMonitors.openfga.port | string | `"metrics"` | Name of the port exposing metrics |
| serviceMonitors.securityOperator | object | `{"enabled":true,"labels":{"app":"security-operator"},"namespace":"platform-mesh-system","path":"/metrics","port":"metrics"}` | security-operator metrics scraping |
| serviceMonitors.securityOperator.enabled | bool | `true` | Enable scraping security-operator |
| serviceMonitors.securityOperator.labels | object | `{"app":"security-operator"}` | Labels to select security-operator metrics service |
| serviceMonitors.securityOperator.namespace | string | `"platform-mesh-system"` | Namespace where security-operator runs |
| serviceMonitors.securityOperator.path | string | `"/metrics"` | Path to metrics endpoint |
| serviceMonitors.securityOperator.port | string | `"metrics"` | Name of the port exposing metrics |

