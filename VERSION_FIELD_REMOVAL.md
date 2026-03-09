# Removing Version Field Duplication

## Issue

The `version` field in `KubeStateMetricsSpec` was redundant since the version is already part of the `image` field.

**Before:**
```yaml
spec:
  version: "2.18.0"                                                    # Redundant!
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0  # Version here
```

## Solution

Removed the `version` field and extract it from the image tag when needed.

**After:**
```yaml
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0  # Single source of truth
```

## Changes Made

### 1. Updated API Types

**File:** `api/v1alpha1/kubestatemetrics_types.go`

**Removed:**
```go
Version string `json:"version"`  // REMOVED
```

**Updated:**
```go
// Image specifies the container image to use for kube-state-metrics
// Should include the full image path with tag (e.g., "registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0")
// +kubebuilder:validation:Required
// +kubebuilder:example="registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0"
Image string `json:"image"`
```

### 2. Updated Controller

**File:** `internal/controller/kubestatemetrics_controller.go`

**Added helper function:**
```go
// extractVersionFromImage extracts the version tag from the container image
// e.g., "registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0" -> "v2.18.0"
func extractVersionFromImage(image string) string {
    parts := strings.Split(image, ":")
    if len(parts) >= 2 {
        return parts[len(parts)-1]
    }
    return "unknown"
}
```

**Updated buildLabels:**
```go
func (r *KubeStateMetricsReconciler) buildLabels(obj *apiv1alpha1.KubeStateMetrics) map[string]string {
    // Extract version from image tag
    version := extractVersionFromImage(obj.Spec.Image)

    return map[string]string{
        "app.kubernetes.io/name":      appName,
        "app.kubernetes.io/component": componentLabel,
        "app.kubernetes.io/version":   version,  // Uses extracted version
    }
}
```

**Added import:**
```go
import (
    // ...
    "strings"  // NEW
    // ...
)
```

### 3. Updated All Examples

Updated all YAML examples to remove the `version` field:

**Files Updated:**
- `examples/with-config-ref.yaml`
- `examples/with-full-config.yaml`
- `examples/shared-config.yaml`
- `test/e2e/onboarding/kubestatemetrics.yaml`

**Before:**
```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
spec:
  version: "2.18.0"
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0
```

**After:**
```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0
```

### 4. Updated Documentation

**Files Updated:**
- `README.md` - Quick start and API reference
- `CONFIG_MANAGEMENT.md` - All examples
- `CONFIG_REFACTORING.md` - Example snippets

## Benefits

1. ✅ **Single Source of Truth** - Version is only specified once (in the image tag)
2. ✅ **Less Redundancy** - No need to keep two fields in sync
3. ✅ **Cleaner API** - Simpler spec structure
4. ✅ **Standard Practice** - Follows Kubernetes conventions (Deployments, Jobs, etc. only specify image)
5. ✅ **Flexibility** - Users can use any image tag format (v2.18.0, 2.18.0, latest, etc.)

## Version Extraction Logic

The controller extracts the version from the image tag for labels:

| Image | Extracted Version |
|-------|------------------|
| `registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0` | `v2.18.0` |
| `my-registry/ksm:2.18.0` | `2.18.0` |
| `my-registry/ksm:latest` | `latest` |
| `my-registry/ksm` (no tag) | `unknown` |

The extracted version is used for:
- `app.kubernetes.io/version` label on all resources

## Migration

This is a **breaking change** for existing resources.

**Old Resource:**
```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
spec:
  version: "2.18.0"
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0
```

**Updated Resource:**
```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0
```

**Migration Steps:**
1. Update CRD: `kubectl apply -f api/crds/manifests/ksm.services.openmcp.cloud_kubestatemetrics.yaml`
2. Update existing resources: Remove `version` field from all KubeStateMetrics resources
3. Redeploy service provider with updated code

## Generated CRD Changes

The CRD no longer has the `version` field in the spec:

**Before:**
```yaml
spec:
  properties:
    version:
      type: string
    image:
      type: string
```

**After:**
```yaml
spec:
  properties:
    image:
      type: string
      description: Should include the full image path with tag
      example: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0
```

## Testing

```bash
# 1. Regenerate code
task generate

# 2. Build
go build -o bin/service-provider-ksm ./cmd/service-provider-ksm
# ✅ Success

# 3. Test with example
kubectl apply -f examples/with-config-ref.yaml

# 4. Verify labels
kubectl get deployment kube-state-metrics -n observability -o yaml | grep "app.kubernetes.io/version"
# Should show: app.kubernetes.io/version: v2.18.0
```

## Comparison with Kubernetes Resources

This change aligns with how Kubernetes native resources work:

**Deployment (Kubernetes native):**
```yaml
spec:
  template:
    spec:
      containers:
        - name: app
          image: nginx:1.19.0  # Version only here
```

**KubeStateMetrics (our CRD):**
```yaml
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0  # Version only here
```

Both follow the same pattern - version is part of the image tag, not a separate field.

---

**Change Date**: 2026-03-06
**Status**: ✅ Complete
**Breaking Change**: Yes (version field removed)
