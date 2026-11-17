{{- define "extension-manager-virtual-workspace.serverUrl" -}}
{{- .Values.deployment.serverUrl | default .Values.kcp.internalFrontProxyUrl | default "https://frontproxy-front-proxy.platform-mesh-system:8443" -}}
{{- end -}}
