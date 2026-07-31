{{/*
Expand the name of the chart.
*/}}
{{- define "cogniflow.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a fully qualified app name.
*/}}
{{- define "cogniflow.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Chart label.
*/}}
{{- define "cogniflow.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "cogniflow.labels" -}}
helm.sh/chart: {{ include "cogniflow.chart" . }}
{{ include "cogniflow.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels.
*/}}
{{- define "cogniflow.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cogniflow.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Orchestrator labels.
*/}}
{{- define "cogniflow.orchestrator.labels" -}}
{{ include "cogniflow.labels" . }}
app.kubernetes.io/component: orchestrator
{{- end -}}

{{/*
Web labels.
*/}}
{{- define "cogniflow.web.labels" -}}
{{ include "cogniflow.labels" . }}
app.kubernetes.io/component: web
{{- end -}}

{{/*
ml-gateway labels.
*/}}
{{- define "cogniflow.mlgateway.labels" -}}
{{ include "cogniflow.labels" . }}
app.kubernetes.io/component: ml-gateway
{{- end -}}
