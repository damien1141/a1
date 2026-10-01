# Helm + Kustomize Packaging

## When to Use Which

| Tool | Use When | Strength |
|---|---|---|
| **Helm** | Packaging for distribution, versioning, repositories | Templating, hooks, OCI registry |
| **Kustomize** | Overlay-based env customization, no templates | Plain YAML, additive, builtin to kubectl |
| **Both** | Large orgs: Helm charts + kustomize overlays per env | Best of both |

## Helm Chart Layout

```
myapp/
├── Chart.yaml              # metadata, appVersion, dependencies
├── values.yaml             # defaults
├── values.schema.json      # input validation (json schema)
├── templates/
│   ├── _helpers.tpl        # named templates
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── ingress.yaml
│   ├── configmap.yaml
│   ├── hpa.yaml
│   ├── serviceaccount.yaml
│   ├── tests/
│   │   └── test-connection.yaml   # `helm test`
│   └── NOTES.txt           # post-install notes
├── charts/                 # vendored dependencies (or use Chart.yaml deps)
└── .helmignore
```

### Chart.yaml (v2)

```yaml
apiVersion: v2
name: myapp
description: MyApp Helm chart
type: application
version: 1.2.0           # chart version (semver)
appVersion: "2.5.0"      # app version (string)
keywords: [web, microservice]
home: https://example.com
sources: [https://github.com/org/myapp]
maintainers:
  - name: DevOps Team
    email: devops@example.com
dependencies:
  - name: postgresql
    version: "15.x.x"
    repository: https://charts.bitnami.com/bitnami
    condition: postgresql.enabled
  - name: redis
    version: "20.x.x"
    repository: https://charts.bitnami.com/bitnami
    condition: redis.enabled
```

### _helpers.tpl (named templates)

```yaml
{{- define "myapp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "myapp.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{ include "myapp.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "myapp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "myapp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
```

### templates/deployment.yaml (excerpt)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "myapp.fullname" . }}
  labels: {{ include "myapp.labels" . | nindent 4 }}
spec:
  {{- if not .Values.autoscaling.enabled }}
  replicas: {{ .Values.replicaCount }}
  {{- end }}
  selector:
    matchLabels: {{ include "myapp.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      annotations:
        # Roll pods when config changes
        checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
      labels: {{ include "myapp.selectorLabels" . | nindent 8 }}
    spec:
      serviceAccountName: {{ include "myapp.serviceAccountName" . }}
      securityContext: {{ toYaml .Values.podSecurityContext | nindent 8 }}
      containers:
        - name: {{ .Chart.Name }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          securityContext: {{ toYaml .Values.securityContext | nindent 12 }}
          resources: {{ toYaml .Values.resources | nindent 12 }}
          livenessProbe: {{ toYaml .Values.livenessProbe | nindent 12 }}
          readinessProbe: {{ toYaml .Values.readinessProbe | nindent 12 }}
```

### values.schema.json — input validation

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["image", "service"],
  "properties": {
    "replicaCount": { "type": "integer", "minimum": 1, "maximum": 100 },
    "image": {
      "type": "object",
      "required": ["repository"],
      "properties": {
        "repository": { "type": "string", "pattern": "^[a-z0-9.-/]+$" },
        "tag": { "type": "string" },
        "pullPolicy": { "enum": ["Always", "IfNotPresent", "Never"] }
      }
    },
    "resources": {
      "type": "object",
      "properties": {
        "requests": { "$ref": "#/definitions/resources" },
        "limits":   { "$ref": "#/definitions/resources" }
      }
    }
  },
  "definitions": {
    "resources": {
      "type": "object",
      "properties": {
        "cpu":    { "type": "string", "pattern": "^[0-9]+m?$" },
        "memory": { "type": "string", "pattern": "^[0-9]+(Mi|Gi)$" }
      }
    }
  }
}
```

### Helm hooks

```yaml
# pre-install, pre-upgrade Job — e.g. DB migration
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ include "myapp.fullname" . }}-migrate
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
    "helm.sh/hook-weight": "0"
    "helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
spec:
  backoffLimit: 3
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: migrate
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          command: ["/app/migrate", "up"]
```

Hook order: `crd-install` → `pre-install` → `post-install` → `pre-delete` → `post-delete` → `pre-upgrade` → `post-upgrade` → `pre-rollback` → `post-rollback` → `test`.

## Helm Workflow

```bash
# Create
helm create myapp

# Lint (always!)
helm lint myapp/

# Render to verify
helm template release myapp/ -f values-prod.yaml

# Install with atomic rollback on failure
helm install myapp myapp/ \
  --namespace production --create-namespace \
  --values values-prod.yaml \
  --atomic --timeout 5m --wait

# Upgrade with diff preview (helm-diff plugin)
helm plugin install https://github.com/databus23/helm-diff
helm diff upgrade myapp myapp/ -f values-prod.yaml

# Upgrade
helm upgrade myapp myapp/ -f values-prod.yaml --atomic --timeout 10m

# Rollback to revision 3
helm rollback myapp 3 --namespace production --wait

# Test (runs templates/tests/*)
helm test myapp -n production --logs
```

## OCI Registry (modern chart distribution)

```bash
# Login
helm registry login registry.example.com -u user -p $TOKEN

# Push
helm package myapp/ --version 1.2.0
helm push myapp-1.2.0.tgz oci://registry.example.com/charts

# Pull / install
helm install myapp oci://registry.example.com/charts/myapp --version 1.2.0
```

OCI is now the recommended distribution; classic chart repos are deprecated.

## Chart Testing (ct)

```bash
brew install chart-testing
ct lint --config ct.yaml
ct lint-and-install --target-branch main
```

```yaml
# ct.yaml
remote: origin
target-branch: main
chart-dirs: [charts]
chart-repos:
  - bitnami=https://charts.bitnami.com/bitnami
helm-extra-args: --timeout 600s
check-version-increment: true
```

## helm-unittest (template unit tests)

```bash
helm plugin install https://github.com/helm-unittest/helm-unittest
helm unittest ./myapp
```

```yaml
# myapp/tests/deployment_test.yaml
suite: deployment tests
templates: [templates/deployment.yaml]
tests:
  - it: should set replicas when autoscaling disabled
    set:
      autoscaling: { enabled: false }
      replicaCount: 5
    asserts:
      - equal: { path: spec.replicas, value: 5 }
  - it: should not set replicas when autoscaling enabled
    set:
      autoscaling: { enabled: true }
    asserts:
      - notExists: { path: spec.replicas }
```

## Kustomize

Plain YAML, no templating — compose via bases + overlays.

```
base/
├── kustomization.yaml
├── deployment.yaml
├── service.yaml
└── configmap.yaml
overlays/
├── staging/
│   ├── kustomization.yaml
│   └── replica-patch.yaml
└── production/
    ├── kustomization.yaml
    ├── replica-patch.yaml
    └── ingress.yaml
```

```yaml
# base/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
commonLabels:
  app.kubernetes.io/name: myapp
resources:
  - deployment.yaml
  - service.yaml
  - configmap.yaml
images:
  - name: registry.example.com/myapp
    newTag: "2.5.0"
```

```yaml
# overlays/production/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: production
resources:
  - ../../base
  - ingress.yaml
patches:
  - replica-patch.yaml
configMapGenerator:
  - name: myapp-config
    behavior: merge
    literals:
      - LOG_LEVEL=warn
      - FEATURE_X=true
```

```yaml
# overlays/production/replica-patch.yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: myapp }
spec:
  replicas: 5
```

```bash
# Build & verify
kustomize build overlays/production | kubectl apply --dry-run=server -f -
kustomize build overlays/production | kubectl apply -f -

# Or via kubectl (kustomize built-in)
kubectl apply -k overlays/production
```

## Helm vs Kustomize Decision

- **Public chart for distribution?** Helm.
- **Internal apps with per-env values only?** Kustomize.
- **Both**: Helm chart as base, kustomize for last-mile env patches.

## Best Practices

1. **Always `helm lint`** in CI; gate on it.
2. **Always ship `values.schema.json`** — catches typos in values before install.
3. **Pin dependency versions** in `Chart.yaml`; run `helm dependency update` in CI.
4. **Use OCI registry** for distribution (chart repos deprecated).
5. **Encrypt secrets** with `helm-secrets` plugin or external-secrets operator — never commit plaintext.
6. **Test with `helm unittest`** for template logic; `ct lint-and-install` for end-to-end.
7. **Use `--atomic --wait`** in production deploys so failures auto-rollback.
8. **Set `revisionHistoryLimit: 10`** on Deployments for `rollout undo`.
9. **`checksum/config` annotation** on pod templates so config changes trigger rolling restart.
10. **Pin `appVersion`** in Chart.yaml and reference it; don't hardcode tags.
