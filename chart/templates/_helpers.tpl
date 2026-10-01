{{/* Renders a non-empty string when any repo uses a token, for use in conditionals. */}}
{{- define "antcd.hasGitTokens" -}}
{{- range .Values.repos }}{{ if or .token .tokenSecret }}true{{ end }}{{ end -}}
{{- end -}}
