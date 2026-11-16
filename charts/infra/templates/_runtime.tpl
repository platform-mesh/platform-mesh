{{/* Chart-owned listener defaults consume the common exposure contract. */}}
{{- define "infra.gateway.listeners" -}}
{{- $listeners := concat (.Values.gatewayApi.listeners | default list) (.Values.gatewayApi.listenersExtra | default list) -}}
{{- $result := list -}}
{{- range $listeners -}}
  {{- $listener := mustDeepCopy . -}}
  {{- $_ := set $listener "port" (.port | default $.Values.gatewayApi.listenerPort | default $.Values.exposure.port | default 443 | int) -}}
  {{- if and (eq .name "terminate-services") (not .hostname) $.Values.exposure.baseDomain -}}
    {{- $_ := set $listener "hostname" (include "common.exposure.host" (dict "exposure" $.Values.exposure "prefix" "*.services")) -}}
  {{- end -}}
  {{- $result = append $result $listener -}}
{{- end -}}
{{- toYaml $result -}}
{{- end -}}

{{/* Include Keycloak's serving path in its advertised URL so OIDC issuers match. */}}
{{- define "infra.keycloak.hostname" -}}
{{- .Values.keycloak.operator.hostname | default (include "common.exposure.url" (dict "exposure" .Values.exposure "path" "/keycloak")) -}}
{{- end -}}
