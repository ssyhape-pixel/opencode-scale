{{/*
Expand the name of the chart.
*/}}
{{- define "opencode-scale.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this
(by the DNS naming spec). If release name contains the chart name it will be
used as a full name.
*/}}
{{- define "opencode-scale.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "opencode-scale.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "opencode-scale.labels" -}}
helm.sh/chart: {{ include "opencode-scale.chart" . }}
{{ include "opencode-scale.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "opencode-scale.selectorLabels" -}}
app.kubernetes.io/name: {{ include "opencode-scale.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Component labels - call with dict "root" $ "component" "router"
*/}}
{{- define "opencode-scale.componentLabels" -}}
{{ include "opencode-scale.labels" .root }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{/*
Component selector labels - call with dict "root" $ "component" "router"
*/}}
{{- define "opencode-scale.componentSelectorLabels" -}}
{{ include "opencode-scale.selectorLabels" .root }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{/*
Create the name of the service account to use.
*/}}
{{- define "opencode-scale.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "opencode-scale.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Return the image for a component.
Usage: {{ include "opencode-scale.image" (dict "imageValues" .Values.router.image "chart" .Chart) }}
*/}}
{{- define "opencode-scale.image" -}}
{{- $tag := default .chart.AppVersion .imageValues.tag -}}
{{- printf "%s:%s" .imageValues.repository $tag -}}
{{- end }}

{{/*
ConfigMap name
*/}}
{{- define "opencode-scale.configmapName" -}}
{{- printf "%s-config" (include "opencode-scale.fullname" .) }}
{{- end }}
