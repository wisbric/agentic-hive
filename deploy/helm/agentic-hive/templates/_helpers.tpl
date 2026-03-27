{{/*
Expand the name of the chart.
*/}}
{{- define "agentic-hive.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "agentic-hive.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "agentic-hive.labels" -}}
helm.sh/chart: {{ include "agentic-hive.name" . }}-{{ .Chart.Version | replace "+" "_" }}
{{ include "agentic-hive.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "agentic-hive.selectorLabels" -}}
app.kubernetes.io/name: {{ include "agentic-hive.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
