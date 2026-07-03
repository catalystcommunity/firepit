{{/*
Expand the name of the chart.
*/}}
{{- define "firepit.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "firepit.fullname" -}}
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
{{- define "firepit.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "firepit.labels" -}}
helm.sh/chart: {{ include "firepit.chart" . }}
{{ include "firepit.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "firepit.selectorLabels" -}}
app.kubernetes.io/name: {{ include "firepit.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "firepit.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "firepit.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
In-cluster URL of firepit-api's HTTP Service, as consumed by both the
webapp's nginx proxy (FIREPIT_API_URL) and any component defaulting
linkkeys.pki.url against this release's own RP.
*/}}
{{- define "firepit.apiUrl" -}}
{{- printf "http://%s-api:%v" (include "firepit.fullname" .) .Values.api.port }}
{{- end }}

{{/*
In-cluster URL of this chart's own linkkeys-rp Service (HTTPS PKI
endpoint), used as the default for linkkeys.pki.url when linkkeysRp.enabled.
*/}}
{{- define "firepit.linkkeysRpUrl" -}}
{{- printf "https://%s-linkkeys-rp:%v" (include "firepit.fullname" .) .Values.linkkeysRp.httpsPort }}
{{- end }}

{{/*
Resolved linkkeys PKI URL: explicit linkkeys.pki.url wins; otherwise fall
back to this chart's own linkkeys-rp Service when it's enabled.
*/}}
{{- define "firepit.resolvedLinkkeysPkiUrl" -}}
{{- if .Values.linkkeys.pki.url -}}
{{ .Values.linkkeys.pki.url }}
{{- else if .Values.linkkeysRp.enabled -}}
{{ include "firepit.linkkeysRpUrl" . }}
{{- end -}}
{{- end }}
