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
{{- $_ := required "exposure.baseDomain is required" .Values.exposure.baseDomain -}}
{{- if .Values.keycloak.baseUrl }}
- {{ printf "--keycloak-base-url=%s" .Values.keycloak.baseUrl | quote }}
{{- else }}
- "--keycloak-base-url={{ .Values.exposure.protocol }}://{{ .Values.exposure.baseDomain }}{{ .Values.exposure.normalizedPort }}/keycloak"
{{- end }}
- "--base-domain={{ .Values.exposure.baseDomain }}{{ .Values.exposure.normalizedPort }}"
{{- end -}}

{{/*
"true" when the LegacyAPIs feature gate is on (default).
*/}}
{{- define "pm-keycloak-operator.legacyAPIs" -}}
{{- if hasKey .Values.featureGates "LegacyAPIs" -}}
{{- if .Values.featureGates.LegacyAPIs }}true{{ end -}}
{{- else -}}true{{- end -}}
{{- end -}}
