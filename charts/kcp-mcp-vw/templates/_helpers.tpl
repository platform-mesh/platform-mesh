{{- define "kcp-mcp-vw.labels" -}}
app: kcp-mcp-vw
app.kubernetes.io/name: kcp-mcp-vw
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}

{{/*
SCAR endpoint: explicit value or /services/access at the external
front-proxy address.
*/}}
{{- define "kcp-mcp-vw.accessURL" -}}
{{ .Values.access.url | default (printf "https://%s:%v/services/access" .Values.external.hostname .Values.external.port) }}
{{- end }}
