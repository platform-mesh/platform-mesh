{{- define "keycloak-operator.watchNamespace" -}}
{{ .Values.watchNamespaces | default .Release.Namespace }}
{{- end }}
