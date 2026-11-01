{{- define "common.tracingEnabled" -}}
{{ include "common.getKeyValue" (dict "Values" .Values "key" "tracing.enabled") }}
{{- end -}}