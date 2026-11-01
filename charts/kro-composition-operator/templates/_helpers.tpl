{{- define "kro-composition-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kro-composition-operator.labels" -}}
app.kubernetes.io/name: {{ include "kro-composition-operator.name" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "kro-composition-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kro-composition-operator.name" . }}
{{- end -}}
