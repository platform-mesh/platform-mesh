{{/* Retain explicit URLs for external identity providers; otherwise derive from exposure. */}}
{{- define "iam-service.keycloak.baseUrl" -}}
{{- if .Values.keycloak.baseUrl -}}
  {{- .Values.keycloak.baseUrl -}}
{{- else -}}
  {{- include "common.exposure.url" (dict "exposure" .Values.exposure "path" "/keycloak") -}}
{{- end -}}
{{- end -}}
