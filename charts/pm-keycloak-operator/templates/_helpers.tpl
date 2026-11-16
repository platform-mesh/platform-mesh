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
{{- with (include "pm-keycloak-operator.keycloakBaseUrl" .) }}
- --keycloak-base-url={{ . }}
{{- end }}
{{- with (include "pm-keycloak-operator.baseDomain" .) }}
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

{{- define "pm-keycloak-operator.keycloakBaseUrl" -}}
{{- .Values.keycloak.baseUrl | default (include "common.exposure.url" (dict "exposure" .Values.exposure "path" "/keycloak")) -}}
{{- end -}}

{{- define "pm-keycloak-operator.baseDomain" -}}
{{- .Values.baseDomain | default (include "common.exposure.authority" (dict "exposure" .Values.exposure)) -}}
{{- end -}}
