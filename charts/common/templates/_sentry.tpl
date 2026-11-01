{{- define "common.sentryEnabled" -}}
{{ include "common.getKeyValue" (dict "Values" .Values "key" "sentry.enabled") }}
{{- end -}}

{{- define "common.sentry-secret" -}}
{{- if and (eq (include "common.sentryEnabled" .) "true") (eq (include "common.externalSecretsEnabled" .) "true") -}}
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: {{ include "common.entity.name" . }}-sentry
  namespace: {{ .Release.Namespace }}
spec:
  data:
  - remoteRef:
      conversionStrategy: Default
      key: {{ include "common.getKeyValue" (dict "Values" .Values "key" "sentry.externalSecrets.secretKey") }}
      property: {{ default (printf "%s-sentry" (include "common.entity.name" . )) (.Values.sentry).secretProperty }}
    secretKey: dsn
  refreshInterval: 10m
  secretStoreRef:
    kind: {{ include "common.getKeyValue" (dict "Values" .Values "key" "externalSecrets.secretStore.kind") }}
    name: {{ include "common.getKeyValue" (dict "Values" .Values "key" "externalSecrets.secretStore.name") }}
  target:
    creationPolicy: Owner
    deletionPolicy: Retain
    name: {{ include "common.entity.name" . }}-sentry
{{- end }}    
{{- end }}

{{- define "common.sentryEnv" }}
{{- if eq (include "common.sentryEnabled" .) "true" -}}
- name: SENTRY_DSN
  valueFrom:
    secretKeyRef:
      name: {{ include "common.entity.name" . }}-sentry
      key: dsn
{{- end -}}
{{- end }}
