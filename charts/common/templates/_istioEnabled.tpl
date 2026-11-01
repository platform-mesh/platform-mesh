{{- define "common.istioEnabled" -}}
{{ include "common.getKeyValue" (dict "Values" .Values "key" "istio.enabled") }}
{{- end -}}