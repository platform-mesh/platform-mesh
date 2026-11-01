{{- define "common.imagePullSecret" }}
{{- if hasKey .Values "imagePullSecret" }}
imagePullSecrets:
  - name: {{ include "common.getKeyValue" (dict "Values" .Values "key" "imagePullSecret") }}
{{- end }}
{{- end -}}