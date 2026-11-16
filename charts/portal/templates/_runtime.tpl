{{- define "portal.baseDomain" -}}
{{- .Values.auth.default.baseDomain | default .Values.exposure.baseDomain -}}
{{- end -}}

{{- define "portal.discoveryUrl" -}}
{{- .Values.auth.default.discoveryUrl | default (include "common.exposure.url" (dict "exposure" .Values.exposure "path" "/keycloak/realms/${org-name}/.well-known/openid-configuration")) -}}
{{- end -}}

{{- define "portal.cookieDomain" -}}
{{- .Values.cookieDomain | default .Values.exposure.baseDomain -}}
{{- end -}}

{{- define "portal.frontendPort" -}}
{{- if .Values.frontendPort -}}
  {{- .Values.frontendPort -}}
{{- else if .Values.exposure.baseDomain -}}
  {{- .Values.exposure.port -}}
{{- else -}}
  8000
{{- end -}}
{{- end -}}

{{- define "portal.protocol" -}}
{{- if .Values.http.protocol -}}
  {{- .Values.http.protocol -}}
{{- else if .Values.exposure.baseDomain -}}
  {{- .Values.exposure.protocol -}}
{{- end -}}
{{- end -}}

{{/* Keep application placeholders literal; explicit extraEnvVars override each derived endpoint. */}}
{{- define "portal.runtimeEnv" -}}
{{- if .Values.exposure.baseDomain -}}
  {{- $explicit := dict -}}
  {{- range .Values.extraEnvVars -}}
    {{- $_ := set $explicit .name true -}}
  {{- end -}}
  {{- $exposure := mustDeepCopy .Values.exposure -}}
  {{- $_ := set $exposure "baseDomain" (printf "${org-subdomain}%s" $exposure.baseDomain) -}}
  {{- $paths := dict "OPENMFP_PORTAL_CONTEXT_IAM_SERVICE_API_URL" "/iam/graphql" "OPENMFP_PORTAL_CONTEXT_CRD_GATEWAY_API_URL" "/gateway/api/clusters/root:orgs:${org-name}/graphql" -}}
  {{- range $name, $path := $paths -}}
    {{- if not (hasKey $explicit $name) }}
- name: {{ $name }}
  value: {{ include "common.exposure.url" (dict "exposure" $exposure "path" $path) | quote }}
    {{- end -}}
  {{- end -}}
{{- end -}}
{{- end -}}
