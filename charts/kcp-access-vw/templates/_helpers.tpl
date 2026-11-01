{{- define "kcp-access-vw.labels" -}}
app: kcp-access-vw
app.kubernetes.io/name: kcp-access-vw
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}

{{/*
Base URL for per-cluster endpoints in SCAR responses: explicit value or
/clusters/ at the external front-proxy address.
*/}}
{{- define "kcp-access-vw.endpointBase" -}}
{{ .Values.server.endpointBase | default (printf "https://%s:%v/clusters/" .Values.external.hostname .Values.external.port) }}
{{- end }}
