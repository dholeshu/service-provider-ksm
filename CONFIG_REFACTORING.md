# Configuration Refactoring Summary

## Changes Made

Successfully refactored the configuration management to use a **separate CRD** (`KubeStateMetricsConfig`) instead of inline configuration.

## Why This Change?

### Problems with Inline Config
1. ❌ Configuration was tightly coupled to deployment
2. ❌ Couldn't share configuration across multiple KSM instances
3. ❌ Configuration changes required redeploying KSM
4. ❌ Large inline YAML made resources hard to read

### Benefits of Separate CRD
1. ✅ **Separation of Concerns**: Config managed independently
2. ✅ **Reusability**: One config can be used by multiple KSM deployments
3. ✅ **Better GitOps**: Config and deployment can be in separate files/repos
4. ✅ **Cleaner Resources**: KSM resource is simpler, just references config
5. ✅ **Flexibility**: Support for multiple config files (custom, standard, additional)

## Architecture

### Before (Inline Config)
```
┌──────────────────────────────────┐
│  KubeStateMetrics                │
│                                  │
│  spec:                           │
│    customResourceStateConfig: |  │  ← Config embedded in deployment
│      # Large YAML here           │
└──────────────────────────────────┘
         │
         ▼
    Creates ConfigMap
         │
         ▼
    Creates Deployment
```

### After (Separate CRD)
```
┌─────────────────────────────┐
│  KubeStateMetricsConfig     │  ← Separate config resource
│                             │
│  spec:                      │
│    customResourceStateConfig│
│    config                   │
│    additionalConfigs        │
└──────────────┬──────────────┘
               │
               │ Creates
               ▼
        ┌──────────────┐
        │  ConfigMap   │
        └──────┬───────┘
               │
               │ Referenced by
               ▼
┌──────────────────────────────┐
│  KubeStateMetrics            │
│                              │
│  spec:                       │
│    configRef:                │  ← Just a reference
│      name: my-config         │
└──────────────────────────────┘
               │
               ▼
    Creates Deployment (mounts ConfigMap)
```

## What Was Created

### 1. New CRD: KubeStateMetricsConfig
**File**: `api/v1alpha1/kubestatemetricsconfig_types.go`

```go
type KubeStateMetricsConfigSpec struct {
    CustomResourceStateConfig string            // For custom resources
    Config                    string            // For standard resources
    AdditionalConfigs         map[string]string // Additional files
}
```

### 2. New Controller: KubeStateMetricsConfigReconciler
**File**: `internal/controller/kubestatemetricsconfig_controller.go`

**Responsibilities**:
- Creates ConfigMap from config spec
- Names ConfigMap: `<config-name>-ksm-config`
- Updates status with ConfigMap name/namespace
- Deletes ConfigMap on config deletion

### 3. Updated KubeStateMetrics API
**File**: `api/v1alpha1/kubestatemetrics_types.go`

**Added**:
```go
type ConfigReference struct {
    Name      string  // Name of KubeStateMetricsConfig
    Namespace string  // Namespace (optional)
}

type KubeStateMetricsSpec struct {
    // ... existing fields ...
    ConfigRef *ConfigReference  // NEW: Reference to config
}
```

**Removed**:
- `CustomResourceStateConfig string` (moved to KubeStateMetricsConfig)

### 4. Updated KubeStateMetrics Controller
**File**: `internal/controller/kubestatemetrics_controller.go`

**Changes**:
- Resolves ConfigRef to get KubeStateMetricsConfig
- Waits if config is not ready
- Gets ConfigMap name from config status
- Mounts ConfigMap in deployment
- Removed inline ConfigMap creation logic

### 5. New Examples
**Directory**: `examples/`

- `with-config-ref.yaml` - Basic usage
- `with-full-config.yaml` - All config options
- `shared-config.yaml` - Config reuse pattern

### 6. Documentation
- `CONFIG_MANAGEMENT.md` - Complete guide to configuration management
- Updated `README.md` - New quick start and API reference
- Updated `IMPLEMENTATION.md` - Architecture documentation

## Usage Examples

### Example 1: Simple Usage

```yaml
# 1. Create config
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: my-config
  namespace: observability
spec:
  customResourceStateConfig: |
    # ... config here

# 2. Reference it from KSM
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: my-ksm
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0
  configRef:
    name: my-config
```

### Example 2: Shared Config

```yaml
# One config
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: shared-config
spec:
  customResourceStateConfig: |
    # Shared config

# Multiple KSM instances using it
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: ksm-prod
spec:
  configRef:
    name: shared-config
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: ksm-dev
spec:
  configRef:
    name: shared-config
```

### Example 3: Multiple Config Files

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: full-config
spec:
  # Custom resource metrics
  customResourceStateConfig: |
    # ... custom config

  # Standard resource metrics
  config: |
    collectors:
      - deployments
      - pods

  # Additional files
  additionalConfigs:
    prometheus-rules.yaml: |
      # ... prometheus rules
```

## How It Works

### 1. Config Creation Flow

1. User creates `KubeStateMetricsConfig`
2. Config controller reconciles
3. Controller creates ConfigMap: `<config-name>-ksm-config`
4. Controller updates config status with ConfigMap details
5. Config is now "Ready"

### 2. KSM Deployment Flow

1. User creates `KubeStateMetrics` with `configRef`
2. KSM controller reconciles
3. Controller fetches referenced `KubeStateMetricsConfig`
4. If config not ready, waits and requeues
5. Gets ConfigMap name from config status
6. Creates Deployment with ConfigMap volume mount
7. KSM pods start with configuration

### 3. Configuration Update Flow

1. User updates `KubeStateMetricsConfig`
2. Config controller reconciles
3. Controller updates ConfigMap
4. KSM deployment picks up changes (if watching ConfigMap)
5. No need to update KSM resource!

## Status Reporting

### KubeStateMetricsConfig Status

```yaml
status:
  configMapName: my-config-ksm-config
  configMapNamespace: observability
  phase: Ready
  conditions:
    - type: Ready
      status: "True"
  observedGeneration: 1
```

### KubeStateMetrics Status

If config not found:
```yaml
status:
  phase: Progressing
  conditions:
    - type: Ready
      status: "False"
      reason: ConfigRefNotFound
      message: "KubeStateMetricsConfig observability/my-config not found"
```

If config not ready:
```yaml
status:
  phase: Progressing
  conditions:
    - type: Ready
      status: "False"
      reason: WaitingForConfig
      message: "Waiting for KubeStateMetricsConfig to be reconciled"
```

## Testing

### Test Manifests Updated

**File**: `test/e2e/onboarding/kubestatemetrics.yaml`

Now includes both:
1. `KubeStateMetricsConfig` resource
2. `KubeStateMetrics` resource with configRef

### Manual Testing

```bash
# 1. Apply config
kubectl apply -f examples/with-config-ref.yaml

# 2. Check config status
kubectl get kubestatemetricsconfig -o wide
kubectl describe kubestatemetricsconfig crossplane-config

# 3. Verify ConfigMap was created
kubectl get configmap crossplane-config-ksm-config -n observability

# 4. Check KSM status
kubectl get kubestatemetrics -o wide
kubectl describe kubestatemetrics crossplane-metrics

# 5. Verify deployment
kubectl get deployment kube-state-metrics -n observability
kubectl get pods -n observability
```

## Generated CRDs

Both CRDs are now available:

1. `api/crds/manifests/ksm.services.openmcp.cloud_kubestatemetricsconfigs.yaml`
2. `api/crds/manifests/ksm.services.openmcp.cloud_kubestatemetrics.yaml`

## Backward Compatibility

### Breaking Change
The `customResourceStateConfig` field has been **removed** from `KubeStateMetricsSpec`.

### Migration Required

**Old:**
```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
spec:
  customResourceStateConfig: |
    # config here
```

**New:**
```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: my-config
spec:
  customResourceStateConfig: |
    # config here
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
spec:
  configRef:
    name: my-config
```

## Files Modified/Created

### Created
- `api/v1alpha1/kubestatemetricsconfig_types.go` - New CRD type
- `internal/controller/kubestatemetricsconfig_controller.go` - New controller
- `examples/with-config-ref.yaml` - Example with config reference
- `examples/with-full-config.yaml` - Full configuration example
- `examples/shared-config.yaml` - Shared config example
- `CONFIG_MANAGEMENT.md` - Configuration management guide

### Modified
- `api/v1alpha1/kubestatemetrics_types.go` - Added ConfigRef, removed inline config
- `internal/controller/kubestatemetrics_controller.go` - Updated to use ConfigRef
- `test/e2e/onboarding/kubestatemetrics.yaml` - Updated test manifest
- `README.md` - Updated with new API and examples

### Deleted
- `examples/kubestatemetrics-full.yaml` - Replaced with new examples
- `examples/kubestatemetrics-simple.yaml` - Replaced with new examples

### Generated
- `api/crds/manifests/ksm.services.openmcp.cloud_kubestatemetricsconfigs.yaml` - New CRD
- `api/v1alpha1/zz_generated.deepcopy.go` - Regenerated with new types

## Benefits Realized

1. ✅ **Better Organization**: Config and deployment are separate concerns
2. ✅ **Config Reuse**: One config → many deployments
3. ✅ **GitOps Friendly**: Config in one repo, deployments in another
4. ✅ **Easier Updates**: Update config without touching deployments
5. ✅ **More Flexible**: Support for multiple config file types
6. ✅ **Better Status**: Each resource reports its own status
7. ✅ **Cleaner YAML**: KSM resources are much smaller and clearer

## Next Steps

1. **Test the implementation**:
   ```bash
   # Build
   go build -o bin/service-provider-ksm ./cmd/service-provider-ksm

   # Apply CRDs
   kubectl apply -f api/crds/manifests/

   # Test examples
   kubectl apply -f examples/with-config-ref.yaml
   ```

2. **Run E2E tests**:
   ```bash
   task test-e2e
   ```

3. **Update existing deployments** to use new config structure

---

**Refactoring Date**: 2026-03-06
**Status**: ✅ Complete and Ready for Testing
