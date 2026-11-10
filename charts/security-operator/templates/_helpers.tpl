{{/*
Feature gate arguments from .Values.featureGates.
keycloak.enabled=false forces Keycloak=false.
*/}}
{{- define "security-operator.featureGates" -}}
{{- $gates := dict -}}
{{- range $name, $enabled := .Values.featureGates -}}
{{- $_ := set $gates $name $enabled -}}
{{- end -}}
{{- if not .Values.keycloak.enabled -}}
{{- $_ := set $gates "Keycloak" false -}}
{{- end -}}
{{- $pairs := list -}}
{{- range $name, $enabled := $gates -}}
{{- $pairs = append $pairs (printf "%s=%s" $name (toString $enabled)) -}}
{{- end -}}
{{- if $pairs }}
- --feature-gates={{ join "," $pairs }}
{{- end -}}
{{- end -}}

{{/*
True when the IdentityProviderConfiguration webhook is served.
*/}}
{{- define "security-operator.webhooks.enabled" -}}
{{- if and .Values.webhooks.enabled .Values.keycloak.enabled -}}true{{- end -}}
{{- end -}}
