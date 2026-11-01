
{{- define "common.serviceBasics" }}
type: {{ include "common.getKeyValue" (dict "Values" .Values "key" "service.type") }}
selector:
    {{- include "common.labelMatcher" . | indent 2 }}
{{- end }}

{{- define "common.servicePorts" }}
- name: http
  protocol: TCP
  port: {{ include "common.getKeyValue" (dict "Values" .Values "key" "service.port") }}
{{- include "common.extraServicePorts" . | nindent 0 }}
{{- end }}

{{- define "common.extraServicePorts" }}
{{- with .Values.extraServicePorts }}
{{- toYaml . }}
{{- end }}
{{- end }}