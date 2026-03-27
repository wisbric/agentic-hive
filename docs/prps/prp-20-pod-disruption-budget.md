# PRP-20: Pod Disruption Budget

## Goal
Add a PodDisruptionBudget to the Helm chart to make availability expectations during cluster maintenance explicit and machine-enforceable.

## Background
The application runs as a single replica with a SQLite file on a PVC. The deployment strategy is `Recreate` (set in `deployment.yaml`), which means zero availability during upgrades — this is intentional and correct for a single-writer SQLite setup. However, without a PDB, a cluster operator draining a node may not realise they are about to disrupt the only replica. A PDB with `maxUnavailable: 1` documents the constraint explicitly: voluntary disruption is allowed (drain can proceed), but the intent is visible in the cluster API. Setting `maxUnavailable: 0` would block voluntary eviction entirely, which is useful if the operator wants to prevent accidental disruptions in production.

The existing Helm chart has no `pdb.yaml` template and no `podDisruptionBudget` values block.

## Requirements

1. Add `deploy/helm/agentic-hive/templates/pdb.yaml`:
   ```yaml
   {{- if .Values.podDisruptionBudget.enabled }}
   apiVersion: policy/v1
   kind: PodDisruptionBudget
   metadata:
     name: {{ include "agentic-hive.fullname" . }}
     labels:
       {{- include "agentic-hive.labels" . | nindent 4 }}
   spec:
     maxUnavailable: {{ .Values.podDisruptionBudget.maxUnavailable }}
     selector:
       matchLabels:
         {{- include "agentic-hive.selectorLabels" . | nindent 8 }}
   {{- end }}
   ```

2. Add to `deploy/helm/agentic-hive/values.yaml` (at the end, after `resources:`):
   ```yaml
   # PodDisruptionBudget
   # Single replica with SQLite. PDB allows disruption since HA is not supported.
   # maxUnavailable: 1 means node drain can proceed (pod may be evicted).
   # Set maxUnavailable: 0 to prevent involuntary eviction and block node drain.
   podDisruptionBudget:
     enabled: true
     maxUnavailable: 1
   ```

3. The PDB must use `policy/v1` (not the deprecated `policy/v1beta1`). `policy/v1` is available in Kubernetes 1.21+ and is the only non-deprecated version in all currently supported Kubernetes releases.

4. No changes to Go source code, Dockerfile, or CI pipeline are required.

## Implementation Notes

- **Label alignment:** `agentic-hive.selectorLabels` is defined in `_helpers.tpl` and already used by `deployment.yaml`. The PDB `selector.matchLabels` must use the same template so it selects the correct pods.
- **`maxUnavailable` type:** Helm will render an integer value from `{{ .Values.podDisruptionBudget.maxUnavailable }}`. This is correct — `maxUnavailable` accepts both integer and percentage string. No quoting needed.
- **`enabled: true` default:** shipping PDB enabled by default is intentional. A cluster operator can set `podDisruptionBudget.enabled: false` to opt out (e.g. in environments that do not support `policy/v1`).
- **Chart.yaml:** no version bump is required unless the project follows semver for chart changes. Leave `Chart.yaml` unchanged unless it is already part of the release process.

## Validation

```bash
# PDB renders when enabled (default)
helm template test ./deploy/helm/agentic-hive | grep -A 12 "kind: PodDisruptionBudget"
# Expect output similar to:
# kind: PodDisruptionBudget
# metadata:
#   name: test-agentic-hive
# spec:
#   maxUnavailable: 1
#   selector:
#     matchLabels:
#       app.kubernetes.io/name: agentic-hive
#       app.kubernetes.io/instance: test

# PDB does not render when disabled
helm template test ./deploy/helm/agentic-hive \
  --set podDisruptionBudget.enabled=false \
  | grep "PodDisruptionBudget" | wc -l
# Expect: 0

# maxUnavailable override works
helm template test ./deploy/helm/agentic-hive \
  --set podDisruptionBudget.maxUnavailable=0 \
  | grep "maxUnavailable"
# Expect: maxUnavailable: 0

# Full template renders without errors
helm template test ./deploy/helm/agentic-hive > /dev/null
# Expect: exit 0, no errors

# Lint passes
helm lint ./deploy/helm/agentic-hive
# Expect: 0 chart(s) linted, 0 chart(s) failed
```

## Out of Scope

- Multi-replica support or horizontal scaling (SQLite is single-writer)
- `minAvailable` variant of PDB (not applicable for single replica)
- PodAntiAffinity or topology spread constraints (no replicas to spread)
- Changes to the `Recreate` deployment strategy
