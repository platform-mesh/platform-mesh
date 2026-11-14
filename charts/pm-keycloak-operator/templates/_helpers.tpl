{{/*
Feature gate arguments from .Values.featureGates.
*/}}
{{- define "pm-keycloak-operator.featureGates" -}}
{{- $pairs := list -}}
{{- range $name, $enabled := .Values.featureGates -}}
{{- $pairs = append $pairs (printf "%s=%s" $name (toString $enabled)) -}}
{{- end -}}
{{- if $pairs }}
- --feature-gates={{ join "," $pairs }}
{{- end -}}
{{- end -}}

{{/*
Keycloak connection arguments shared by every command.
*/}}
{{- define "pm-keycloak-operator.keycloakArgs" -}}
{{- with .Values.keycloak.baseUrl }}
- --keycloak-base-url={{ . }}
{{- end }}
{{- with .Values.baseDomain }}
- --base-domain={{ . }}
{{- end }}
{{- end -}}

{{/*
"true" when the LegacyAPIs feature gate is on (default).
*/}}
{{- define "pm-keycloak-operator.legacyAPIs" -}}
{{- if hasKey .Values.featureGates "LegacyAPIs" -}}
{{- if .Values.featureGates.LegacyAPIs }}true{{ end -}}
{{- else -}}true{{- end -}}
{{- end -}}
