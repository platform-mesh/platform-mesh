{{- define "common.deploymentBasics" }}
strategy:
  rollingUpdate:
    maxSurge: {{ include "common.getKeyValue" (dict "Values" .Values "key" "deployment.maxSurge") }}
    maxUnavailable: {{ include "common.getKeyValue" (dict "Values" .Values "key" "deployment.maxUnavailable") }}
  type: {{ include "common.getKeyValue" (dict "Values" .Values "key" "deployment.strategy") }}
revisionHistoryLimit: {{ include "common.getKeyValue" (dict "Values" .Values "key" "deployment.revisionHistoryLimit") }}
selector:
  matchLabels:
    {{- include "common.labelMatcher" .  | indent 4 }}
{{- end }}
{{- define "common.labelMatcher" }}
app: {{ include "common.entity.name" . }}
{{- end }}

{{- define "common.podBasics" }}
name: {{ include "common.entity.name" . }}
image: {{ include "common.image" . }}
{{- include "common.imagePullPolicy" . }}
{{- include "common.resources" . }}
{{- end }}
{{- define "common.resources" }}
resources:
  limits:
    {{ if not (eq (include "common.getKeyValue" (dict "Values" .Values "key" "deployment.resources.limits.cpu")) "") }}
    cpu: {{ include "common.getKeyValue" (dict "Values" .Values "key" "deployment.resources.limits.cpu") }}
    {{- end }}
    memory: {{ include "common.getKeyValue" (dict "Values" .Values "key" "deployment.resources.limits.memory") }}
  requests:
    cpu: {{ include "common.getKeyValue" (dict "Values" .Values "key" "deployment.resources.requests.cpu") }}
    memory: {{ include "common.getKeyValue" (dict "Values" .Values "key" "deployment.resources.requests.memory") }}
{{- end }}
{{- define "common.ports" }}
- name: http
  containerPort: {{ include "common.getKeyValue" (dict "Values" .Values "key" "port") }}
  protocol: TCP
{{- include "common.PortsMetricsHealth" (dict "Values" .Values) }}
{{- end}}

{{- define "common.technicalIssuers" }}
{{- $technicalIssuers := list }}
{{- range $issuer, $config := .Values.trustedIssuers }}
{{- if $config.isTechnicalIssuer }}
{{- $technicalIssuers = append $technicalIssuers  $config.url}}
{{- end}}
{{- end}}
{{- join "," $technicalIssuers }}
{{- end}}

{{- define "common.commonArgs" }}
{{- include "common.observabilityArgs" . | nindent 2 }}
{{- include "common.collectorArgs" . | nindent 2 }}
{{- include "common.extraArgs" . | nindent 2 }}
{{- end }}

{{- define "common.commonOperatorArgs" }}
{{- include "common.leaderElectArg" . | indent 2 }}
{{- include "common.commonArgs" . }}
{{- end }}

{{- define "common.leaderElectArg" }}
{{- if eq (include "common.getKeyValue" (dict "Values" .Values "key" "operator.leaderElect")) "true" }}
- --leader-elect
{{- end -}}
{{- end }}

{{- define "common.collectorArgs" }}
{{- if eq (include "common.tracingEnabled" .) "true" }}
- --tracing-enabled={{ include "common.tracingEnabled" .}}
- --tracing-config-service-name={{ include "common.entity.name" .}}.{{ .Release.Namespace}}
- --tracing-config-service-version={{ include "common.image.tag" . }}
- --tracing-config-collector-endpoint={{ include "common.getKeyValue" (dict "Values" .Values "key" "tracing.collector.endpoint") }}
{{- end }}
{{- end }}

{{- define "common.observabilityArgs" }}
- --metrics-bind-address=:{{ include "common.getKeyValue" (dict "Values" .Values "key" "metrics.port") }}
- --health-probe-bind-address=:{{ include "common.getKeyValue" (dict "Values" .Values "key" "health.port") }}
- --log-level={{ include "common.getKeyValue" (dict "Values" .Values "key" "log.level") }}
{{- $noJson := include "common.getKeyValue" (dict "Values" .Values "key" "log.noJson") }}
{{- if eq $noJson "true" }}
- --no-json
{{- end }}
- --region={{ include "common.getKeyValue" (dict "Values" .Values "key" "region") }}
- --environment={{ include "common.getKeyValue" (dict "Values" .Values "key" "environment") }}
- --image-tag={{ include "common.image.tag" . }}
- --image-name="{{ include "common.image.name" . }}"
- --shutdown-timeout={{ include "common.getKeyValue" (dict "Values" .Values "key" "operator.shutdownTimeout") }}
- --max-concurrent-reconciles={{ include "common.getKeyValue" (dict "Values" .Values "key" "operator.maxConcurrentReconciles") }}
{{- $enableHttp2 := include "common.getKeyValue" (dict "Values" .Values "key" "enableHttp2") }}
{{- if eq $enableHttp2 "false" }}
- --enable-http2=false
{{- end }}
{{- end }}

{{- define "common.basicEnvironment" }}
{{ include "common.sentryEnv" . }}
{{ include "common.extraEnvs" . }}
{{- end }}
{{- define "common.basicJob" }}
- name: ISTIO_QUIT_API
  value: http://127.0.0.1:15020
{{- end }}
{{- define "common.healthAndReadiness" }}
{{ include "common.operatorHealthAndReadyness" . }}
{{- end }}
{{- define "common.operatorHealthAndReadyness" }}
livenessProbe:
  httpGet:
    path: {{ include "common.getKeyValue" (dict "Values" .Values "key" "health.liveness.path") }}
    port: {{ include "common.getKeyValue" (dict "Values" .Values "key" "health.port") }}
  failureThreshold: {{ include "common.getKeyValue" (dict "Values" .Values "key" "health.liveness.failureThreshold") }}
  periodSeconds: {{ include "common.getKeyValue" (dict "Values" .Values "key" "health.periodSeconds") }}
startupProbe:
  httpGet:
    path: {{ include "common.getKeyValue" (dict "Values" .Values "key" "health.startup.path") }}
    port: {{ include "common.getKeyValue" (dict "Values" .Values "key" "health.port") }}
  failureThreshold: {{ include "common.getKeyValue" (dict "Values" .Values "key" "health.startup.failureThreshold") }}
  periodSeconds: {{ include "common.getKeyValue" (dict "Values" .Values "key" "health.periodSeconds") }}
readinessProbe:
  httpGet:
    path: {{ include "common.getKeyValue" (dict "Values" .Values "key" "health.readiness.path") }}
    port: {{ include "common.getKeyValue" (dict "Values" .Values "key" "health.port") }}
  initialDelaySeconds: {{ include "common.getKeyValue" (dict "Values" .Values "key" "health.readiness.initialDelaySeconds") }}
  periodSeconds: {{ include "common.getKeyValue" (dict "Values" .Values "key" "health.periodSeconds") }}
{{- end }}
{{- define "common.terminationGracePeriodSeconds" -}}
{{ .Values.terminationGracePeriodSeconds | default 10 }}
{{- end }}
{{- define "common.imagePullPolicy" }}
imagePullPolicy: {{ include "common.getKeyValue" (dict "Values" .Values "key" "imagePullPolicy") }}
{{- end }}
{{- define "common.PortsMetricsHealth" }}

{{- $containerPort := include "common.getKeyValue" (dict "Values" .Values "key" "port") }}
{{- $metricsPort := include "common.getKeyValue" (dict "Values" .Values "key" "metrics.port") }}
{{- if not (eq $containerPort $metricsPort) }}
- name: metrics
  containerPort: {{ $metricsPort }}
  protocol: TCP
{{- end }}

{{- $healthPort := include "common.getKeyValue" (dict "Values" .Values "key" "health.port") }}
{{- if not (eq $containerPort $healthPort) }}
- name: health-port
  containerPort: {{ $healthPort }}
  protocol: TCP
{{- end }}
{{- end -}}


{{- define "common.container.securityContext" }}
securityContext:
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault
{{- end }}


{{- define "common.pod.securityContext" }}
securityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault
serviceAccountName: {{ include "common.entity.name" . }}
automountServiceAccountToken: {{ not (eq (.Values.security).mountServiceAccountToken false) }}
{{- end }}

{{- define "common.spec.securityContext" -}}
securityContext:
  runAsUser: {{ include "common.getKeyValue" (dict "Values" .Values "key" "securityContext.runAsUser") }}
  runAsGroup: {{ include "common.getKeyValue" (dict "Values" .Values "key" "securityContext.runAsGroup") }}
  fsGroup: {{ include "common.getKeyValue" (dict "Values" .Values "key" "securityContext.fsGroup") }}
{{- end }}

{{- define "common.image.tag" -}}
{{- if (.Values.image).tag }}
{{- .Values.image.tag }}
{{- else }}
{{- .Chart.AppVersion }}
{{- end }}
{{- end }}

{{- define "common.image.name" -}}
{{- printf "%s/%s" .Values.image.registry .Values.image.repository -}}
{{- end }}

{{- define "common.image" -}}
{{- $sep := ":" -}}
{{- $term := include "common.image.tag" . -}}
{{- if (.Values.image).digest -}}
{{- $sep = "@" -}}
{{- $term = .Values.image.digest -}}
{{- end -}}
{{- printf "%s%s%s" (include "common.image.name" .) $sep $term -}}
{{- end }}
