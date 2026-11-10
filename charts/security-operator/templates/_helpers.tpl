{{/*
Feature gate arguments from .Values.featureGates.
*/}}
{{- define "security-operator.featureGates" -}}
{{- $pairs := list -}}
{{- range $name, $enabled := .Values.featureGates -}}
{{- $pairs = append $pairs (printf "%s=%s" $name (toString $enabled)) -}}
{{- end -}}
{{- if $pairs }}
- --feature-gates={{ join "," $pairs }}
{{- end -}}
{{- end -}}
