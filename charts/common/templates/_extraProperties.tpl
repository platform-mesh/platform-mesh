{{- define "common.extraEnvs" }}
{{- with .Values.extraEnvs -}}
{{ toYaml . }}
{{- end -}}
{{- end }}

{{- define "common.extraArgs" }}
{{- with .Values.extraArgs }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{- define "common.extraVolumes" }}
{{- with .Values.extraVolumes -}}
{{ toYaml . }}
{{- end -}}
{{- end }}

{{- define "common.extraVolumeMounts" }}
{{- with .Values.extraVolumeMounts -}}
{{ toYaml . }}
{{- end -}}
{{- end }}