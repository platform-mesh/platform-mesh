{{- define "backup-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "backup-operator.labels" -}}
app.kubernetes.io/name: {{ include "backup-operator.name" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "backup-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "backup-operator.name" . }}
{{- end -}}
