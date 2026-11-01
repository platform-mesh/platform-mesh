{{- define "common.externalSecretsEnabled" -}}
{{ include "common.getKeyValue" (dict "Values" .Values "key" "externalSecrets.enabled") }}
{{- end -}}