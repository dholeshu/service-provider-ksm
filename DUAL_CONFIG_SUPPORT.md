# Dual Configuration Support

## Overview

The service provider now fully supports **both** standard Kubernetes resource monitoring AND custom resource monitoring, either separately or together.

## Configuration Types Supported

### 1. Custom Resources Only (Default)

Monitor only custom resources (CRDs):

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
spec:
  customResourceStateConfig: |
    kind: CustomResourceStateMetrics
    spec:
      resources:
        - groupVersionKind:
            group: my.group
            kind: MyKind
          metrics: []
```

**Result:**
- ConfigMap contains: `custom-resource-state-config.yaml`
- KSM args: `--custom-resource-state-only=true --custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state-config.yaml`
- Monitors: Only custom resources

### 2. Standard Resources Only

Monitor only standard Kubernetes resources (Pods, Deployments, etc.):

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
spec:
  config: |
    resources:
      - deployments
      - pods
      - nodes
```

**Result:**
- ConfigMap contains: `config.yaml`
- KSM args: `--config=/etc/kube-state-metrics/config.yaml`
- Monitors: Only standard Kubernetes resources

### 3. Both Custom and Standard Resources (Recommended)

Monitor both custom and standard resources:

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
spec:
  customResourceStateConfig: |
    kind: CustomResourceStateMetrics
    spec:
      resources:
        - groupVersionKind:
            group: my.group
            kind: MyKind
          metrics: []
  config: |
    resources:
      - deployments
      - pods
```

**Result:**
- ConfigMap contains: `custom-resource-state-config.yaml` AND `config.yaml`
- KSM args: `--custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state-config.yaml --config=/etc/kube-state-metrics/config.yaml`
- Monitors: Both custom and standard resources

## Implementation Details

### Controller Logic

The `KubeStateMetricsConfigReconciler` creates a ConfigMap with all provided configs:

```go
// In KubeStateMetricsConfigReconciler.CreateOrUpdate()
data := make(map[string]string)

if configObj.Spec.CustomResourceStateConfig != "" {
    data["custom-resource-state-config.yaml"] = configObj.Spec.CustomResourceStateConfig
}

if configObj.Spec.Config != "" {
    data["config.yaml"] = configObj.Spec.Config
}

// Additional configs
for filename, content := range configObj.Spec.AdditionalConfigs {
    data[filename] = content
}
```

### Argument Generation

The `KubeStateMetricsReconciler` dynamically generates arguments:

```go
// In applyDeploymentSpec()

// Determine mode based on what configs exist
customResourceStateOnly := true
if hasConfig && ksmConfig != nil && ksmConfig.Spec.Config != "" {
    customResourceStateOnly = false  // We have standard config
}

if customResourceStateOnly {
    args = append(args, "--custom-resource-state-only=true")
}

// Add config file args
if hasConfig && ksmConfig != nil {
    if ksmConfig.Spec.CustomResourceStateConfig != "" {
        args = append(args, "--custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state-config.yaml")
    }
    if ksmConfig.Spec.Config != "" {
        args = append(args, "--config=/etc/kube-state-metrics/config.yaml")
    }
}
```

## Use Cases

### Use Case 1: Monitor Only Custom Resources

**Scenario:** You only want metrics from your custom resources (e.g., Crossplane resources)

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: crossplane-only
spec:
  customResourceStateConfig: |
    kind: CustomResourceStateMetrics
    spec:
      resources:
        - groupVersionKind:
            group: account.btp.sap.crossplane.io
            version: "*"
            kind: "*"
          metricNamePrefix: crossplane_btp
          metrics: []
```

### Use Case 2: Monitor Only Standard Resources

**Scenario:** You want standard Kubernetes metrics (Pods, Deployments, etc.)

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: standard-only
spec:
  config: |
    resources:
      - deployments
      - pods
      - nodes
      - services
```

### Use Case 3: Monitor Everything

**Scenario:** You want metrics from both custom and standard resources

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: complete-monitoring
spec:
  customResourceStateConfig: |
    kind: CustomResourceStateMetrics
    spec:
      resources:
        - groupVersionKind:
            group: account.btp.sap.crossplane.io
            version: "*"
            kind: "*"
          metricNamePrefix: crossplane
          metrics: []
  config: |
    resources:
      - deployments
      - pods
      - nodes
      - services
      - configmaps
```

## Examples

See the `examples/` directory:

1. **with-config-ref.yaml** - Custom resources only
2. **with-full-config.yaml** - Both custom and standard resources
3. **standard-resources.yaml** - Standard resources only (NEW)
4. **shared-config.yaml** - Shared config pattern

## Migration from Previous Version

### If you had customResourceStateOnly: false

**Before:**
```yaml
spec:
  customResourceStateOnly: false
  configRef:
    name: my-config
```

**Now:**
Just ensure your config has both types:
```yaml
# KubeStateMetricsConfig
spec:
  customResourceStateConfig: |
    # custom config
  config: |
    # standard config
```

The controller automatically detects and sets the mode.

## Testing

### Test Custom Resources Only

```bash
kubectl apply -f examples/with-config-ref.yaml
kubectl logs -n observability deployment/kube-state-metrics | grep "custom-resource-state-only"
# Should see: --custom-resource-state-only=true
```

### Test Standard Resources Only

```bash
kubectl apply -f examples/standard-resources.yaml
kubectl logs -n monitoring deployment/kube-state-metrics | grep "custom-resource-state-only"
# Should NOT see --custom-resource-state-only
```

### Test Both

```bash
kubectl apply -f examples/with-full-config.yaml
kubectl logs -n monitoring deployment/kube-state-metrics
# Should see both:
# --custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state-config.yaml
# --config=/etc/kube-state-metrics/config.yaml
# Should NOT see --custom-resource-state-only
```

## Benefits

1. ✅ **Flexible** - Monitor custom, standard, or both
2. ✅ **Automatic Mode Detection** - No need to manually set `customResourceStateOnly`
3. ✅ **Single ConfigMap** - All configs in one place
4. ✅ **Proper KSM Flags** - Correct arguments for each scenario
5. ✅ **Reusable** - Same config can be shared across deployments

## KSM Flag Reference

| Scenario | Flags Used |
|----------|-----------|
| Custom only | `--custom-resource-state-only=true --custom-resource-state-config-file=...` |
| Standard only | `--config=...` |
| Both | `--custom-resource-state-config-file=... --config=...` |

---

**Implementation Date**: 2026-03-06
**Status**: ✅ Complete and Ready for Testing
