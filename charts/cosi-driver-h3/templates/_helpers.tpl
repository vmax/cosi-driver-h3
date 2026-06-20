{{- define "cosi-driver-h3.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "cosi-driver-h3.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "cosi-driver-h3.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "cosi-driver-h3.labels" -}}
app.kubernetes.io/name: {{ include "cosi-driver-h3.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "cosi-driver-h3.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cosi-driver-h3.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "cosi-driver-h3.serviceAccountName" -}}
{{- default (include "cosi-driver-h3.fullname" .) .Values.serviceAccount.name -}}
{{- end -}}

{{- define "cosi-driver-h3.secretName" -}}
{{- if .Values.credentials.existingSecret -}}
{{- .Values.credentials.existingSecret -}}
{{- else -}}
{{- printf "%s-credentials" (include "cosi-driver-h3.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "cosi-driver-h3.driverImage" -}}
{{- printf "%s:%s" .Values.driver.image.repository (default .Chart.AppVersion .Values.driver.image.tag) -}}
{{- end -}}
