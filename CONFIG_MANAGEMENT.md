# Configuration Management with KubeStateMetricsConfig

## Overview

The service provider uses a **separate CRD for configuration management** (`KubeStateMetricsConfig`) that is referenced by `KubeStateMetrics` resources. This design provides:

- ✅ **Separation of Concerns**: Configuration is managed independently from deployment
- ✅ **Reusability**: Multiple KSM deployments can share the same configuration
- ✅ **Flexibility**: Support for custom resource state config, standard config, and additional files
- ✅ **Better GitOps**: Configuration changes don't require redeploying KSM

## Architecture

```
┌─────────────────────────────────┐
│  KubeStateMetricsConfig         │
│  (Configuration CRD)            │
│                                 │
│  spec:                          │
│    customResourceStateConfig    │
│    config                       │
│    additionalConfigs            │
└──────────────┬──────────────────┘
               │
               │ Creates
               ▼
        ┌──────────────┐
        │  ConfigMap   │
        │  (Data)      │
        └──────┬───────┘
               │
               │ Referenced by
               ▼
┌──────────────────────────────────┐
│  KubeStateMetrics                │
│  (Deployment CRD)                │
│                                  │
│  spec:                           │
│    configRef:                    │
│      name: <config-name>         │
│      namespace: <namespace>      │
└──────────────────────────────────┘
```

## KubeStateMetricsConfig CRD

### Purpose

Manages the configuration files for kube-state-metrics and creates a ConfigMap containing all configuration data.

### Spec Fields

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: my-config
  namespace: observability
spec:
  # Custom resource state metrics configuration
  # This file will be created as: custom-resource-state-config.yaml
  # For monitoring custom resources (CRDs)
  customResourceStateConfig: |
    kind: CustomResourceStateMetrics
    spec:
      resources:
        - groupVersionKind:
            group: my.group
            version: "*"
            kind: "MyKind"
          metrics:
            - name: my_metric
              help: "My metric"
              each:
                type: Gauge
                gauge:
                  path: [spec, value]

  # Standard kube-state-metrics configuration (optional)
  # This file will be created as: config.yaml
  # For monitoring standard Kubernetes resources (Pods, Deployments, etc.)
  config: |
    resources:
      - deployments
      - pods
      - nodes

  # Additional configuration files (optional)
  # Key = filename, Value = file content
  additionalConfigs:
    my-rules.yaml: |
      groups:
        - name: my-rules
          rules: []
```

### Configuration Types

**1. Custom Resources Only** (default)
```yaml
spec:
  customResourceStateConfig: |
    # Custom resource config here
  # No standard config
```
Result: Only monitors custom resources (CRDs). `--custom-resource-state-only=true` flag is used.

**2. Standard Resources Only**
```yaml
spec:
  config: |
    resources:
      - deployments
      - pods
  # No custom resource config
```
Result: Only monitors standard Kubernetes resources. No `--custom-resource-state-only` flag.

**3. Both Custom and Standard Resources** (recommended)
```yaml
spec:
  customResourceStateConfig: |
    # Custom resource config here
  config: |
    resources:
      - deployments
      - pods
```
Result: Monitors both custom and standard resources. No `--custom-resource-state-only` flag.

### Status Fields

```yaml
status:
  # Name of the ConfigMap created by this controller
  configMapName: my-config-ksm-config

  # Namespace where the ConfigMap was created
  configMapNamespace: observability

  # Phase: Ready, Progressing, Terminating
  phase: Ready

  # Conditions with detailed status information
  conditions:
    - type: Ready
      status: "True"
      reason: ReconcileSuccess
      message: "Domain Service is ready"

  # Last reconciled generation
  observedGeneration: 1
```

### Controller Behavior

1. **Creates ConfigMap**: `<config-name>-ksm-config` in the specified namespace
2. **Populates Data**:
   - `custom-resource-state-config.yaml`: From `spec.customResourceStateConfig` (if provided)
   - `config.yaml`: From `spec.config` (if provided)
   - Additional files: From `spec.additionalConfigs` (if provided)
3. **Updates Status**: Sets `configMapName` and `configMapNamespace`
4. **On Delete**: Removes the ConfigMap

### KubeStateMetrics Controller Behavior

The KubeStateMetrics controller dynamically configures kube-state-metrics based on the available configuration:

1. **Checks Config Files**: Reads the referenced KubeStateMetricsConfig to see what's available
2. **Sets Arguments**:
   - If `customResourceStateConfig` exists: Adds `--custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state-config.yaml`
   - If `config` exists: Adds `--config=/etc/kube-state-metrics/config.yaml`
3. **Determines Mode**:
   - If **only** `customResourceStateConfig` exists: Uses `--custom-resource-state-only=true`
   - If `config` exists: Does NOT use `--custom-resource-state-only` (monitors all resources)
4. **Mounts ConfigMap**: All config files from the ConfigMap are mounted at `/etc/kube-state-metrics/`

### ConfigMap Naming

The controller creates a ConfigMap with the name: `<config-resource-name>-ksm-config`

Example:
- Config name: `crossplane-config`
- ConfigMap name: `crossplane-config-ksm-config`

## KubeStateMetrics CRD Updates

### ConfigRef Field

The `KubeStateMetrics` CRD now has a `configRef` field to reference a `KubeStateMetricsConfig`:

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: my-ksm
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0

  # Reference to KubeStateMetricsConfig
  configRef:
    name: my-config               # Name of the KubeStateMetricsConfig
    namespace: observability       # Namespace (optional, defaults to KSM namespace)

  # ... other fields
```

### Controller Behavior

1. **Fetches Config**: Gets the referenced `KubeStateMetricsConfig`
2. **Waits if Needed**: If config is not ready, waits and requeues
3. **Uses ConfigMap**: Mounts the ConfigMap created by the config controller
4. **Creates Deployment**: With volume mount pointing to the ConfigMap

### Volume Mount

The controller automatically creates:

**Volume:**
```yaml
volumes:
  - name: config
    configMap:
      name: <referenced-config-map-name>
```

**Volume Mount:**
```yaml
volumeMounts:
  - name: config
    mountPath: /etc/kube-state-metrics/
```

### Container Args

The controller automatically adds the appropriate flags:
- `--custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state-config.yaml`

## Usage Patterns

### Pattern 1: Simple Config

One config, one deployment:

```yaml
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: my-config
  namespace: observability
spec:
  customResourceStateConfig: |
    # ... config here
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

### Pattern 2: Shared Config

One config, multiple deployments:

```yaml
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: shared-config
  namespace: observability
spec:
  customResourceStateConfig: |
    # ... shared config
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: ksm-cluster1
spec:
  # ...
  configRef:
    name: shared-config
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: ksm-cluster2
spec:
  # ...
  configRef:
    name: shared-config
```

### Pattern 3: Cross-Namespace Reference

Config in one namespace, KSM in another:

```yaml
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: global-config
  namespace: config-namespace
spec:
  customResourceStateConfig: |
    # ... config
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: my-ksm
  namespace: app-namespace
spec:
  # ...
  configRef:
    name: global-config
    namespace: config-namespace  # Explicit cross-namespace reference
```

## Benefits

### 1. Configuration Reusability

Multiple KSM deployments can share the same configuration:

```yaml
# One config for monitoring Crossplane resources
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: crossplane-monitoring
spec:
  customResourceStateConfig: |
    # Crossplane resource metrics config

# Use in multiple clusters
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: ksm-prod
spec:
  configRef:
    name: crossplane-monitoring
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: ksm-dev
spec:
  configRef:
    name: crossplane-monitoring
```

### 2. Independent Updates

Update configuration without redeploying KSM:

1. Update `KubeStateMetricsConfig`
2. Config controller updates ConfigMap
3. KSM pods automatically pick up new config (if watching ConfigMap changes)

### 3. GitOps Friendly

Separate configuration from deployment:

```
configs/
  crossplane-config.yaml      # Configuration
  app-config.yaml
deployments/
  ksm-prod.yaml               # Deployment
  ksm-dev.yaml
```

### 4. Validation

Each CRD can have its own validation rules:
- `KubeStateMetricsConfig`: Validates configuration syntax
- `KubeStateMetrics`: Validates deployment parameters

### 5. Status Reporting

Each resource reports its own status:
- Config: Reports ConfigMap creation status
- KSM: Reports deployment readiness

## Migration from Inline Config

If you were using inline `customResourceStateConfig` before:

**Old (deprecated):**
```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
spec:
  customResourceStateConfig: |
    # inline config
```

**New (recommended):**
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

## Troubleshooting

### Config Not Found

**Error:** `ConfigRefNotFound: KubeStateMetricsConfig observability/my-config not found`

**Solution:**
1. Check if the config exists: `kubectl get kubestatemetricsconfig my-config -n observability`
2. Verify the namespace is correct
3. Ensure the config is in the same cluster

### Config Not Ready

**Status:** `WaitingForConfig: Waiting for KubeStateMetricsConfig to be reconciled`

**Solution:**
1. Check config status: `kubectl describe kubestatemetricsconfig my-config`
2. Verify config controller is running
3. Check for errors in config reconciliation

### ConfigMap Not Created

**Solution:**
1. Check KubeStateMetricsConfig status
2. Look at controller logs
3. Verify namespace exists

## Examples

See the `examples/` directory:
- `with-config-ref.yaml` - Basic usage with ConfigRef
- `with-full-config.yaml` - Full configuration with all options
- `shared-config.yaml` - Reusing config across deployments
