# KubeStateMetrics Service Provider Implementation

## Overview

This document describes the implementation of the kube-state-metrics service provider, which manages the complete lifecycle of kube-state-metrics deployments on OpenMCP clusters.

## Architecture

The service provider follows the OpenMCP Service Provider pattern and consists of:

1. **API Layer** (`api/v1alpha1/`)
   - `KubeStateMetrics` CRD - Defines the desired state
   - `ProviderConfig` CRD - Provider-specific configuration

2. **Controller Layer** (`internal/controller/`)
   - Reconciliation logic for KubeStateMetrics resources
   - Creates and manages all required Kubernetes resources

3. **Managed Resources**
   The controller creates and manages the following resources:
   - **Namespace** - Target namespace for KSM deployment
   - **ServiceAccount** - Identity for KSM pods
   - **ClusterRole** - Permissions to read Kubernetes resources
   - **ClusterRoleBinding** - Binds ServiceAccount to ClusterRole
   - **ConfigMap** - Contains custom resource state configuration
   - **Deployment** - Runs the kube-state-metrics pods
   - **Service** - Exposes metrics endpoints

## API Specification

### KubeStateMetricsSpec

```go
type KubeStateMetricsSpec struct {
    // Version specifies the kube-state-metrics version to deploy (required)
    Version string

    // Image specifies the container image to use (required)
    Image string

    // Namespace specifies the target namespace (default: "observability")
    Namespace string

    // Replicas specifies the number of replicas (default: 1)
    Replicas *int32

    // ImagePullSecrets for private registries
    ImagePullSecrets []corev1.LocalObjectReference

    // Resources defines resource requests and limits
    Resources *corev1.ResourceRequirements

    // CustomResourceStateOnly when true, only monitors custom resources
    CustomResourceStateOnly *bool

    // CustomResourceStateConfig contains the metrics configuration
    CustomResourceStateConfig string

    // Args specifies additional command-line arguments
    Args []string

    // NodeSelector for pod scheduling
    NodeSelector map[string]string

    // SecurityContext for the pod
    SecurityContext *corev1.PodSecurityContext
}
```

### KubeStateMetricsStatus

```go
type KubeStateMetricsStatus struct {
    // Conditions represent the current state
    Conditions []metav1.Condition

    // ObservedGeneration is the last reconciled generation
    ObservedGeneration int64

    // Phase is the current phase (Ready, Progressing, Terminating)
    Phase string
}
```

## Controller Logic

### CreateOrUpdate Flow

1. **Status Update**: Set status to "Progressing"
2. **Namespace Creation**: Ensure target namespace exists
3. **ServiceAccount Creation**: Create KSM service account
4. **ClusterRole Creation**: Create ClusterRole with required permissions
5. **ClusterRoleBinding Creation**: Bind ServiceAccount to ClusterRole
6. **ConfigMap Creation**: Create ConfigMap with custom resource state config (if provided)
7. **Deployment Creation**: Create KSM deployment with configured spec
8. **Service Creation**: Create Service to expose metrics endpoints
9. **Status Check**: Check deployment readiness
10. **Status Update**: Set status to "Ready" when all replicas are ready

### Delete Flow

1. **Status Update**: Set status to "Terminating"
2. **Resource Deletion**: Delete resources in reverse order:
   - Service
   - Deployment
   - ConfigMap
   - ClusterRoleBinding
   - ClusterRole
   - ServiceAccount
3. **Verification**: Wait for all resources to be fully deleted

## Default Configuration

The controller applies these defaults based on the POC implementation:

### Labels
```yaml
app.kubernetes.io/name: kube-state-metrics
app.kubernetes.io/component: exporter
app.kubernetes.io/version: <spec.version>
```

### Security Context
```yaml
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  runAsUser: 65534
  seccompProfile:
    type: RuntimeDefault
```

### Probes
```yaml
livenessProbe:
  httpGet:
    path: /livez
    port: http-metrics
  initialDelaySeconds: 5
  timeoutSeconds: 5

readinessProbe:
  httpGet:
    path: /readyz
    port: telemetry
  initialDelaySeconds: 5
  timeoutSeconds: 5
```

### Ports
- **8080** (http-metrics) - Metrics endpoint
- **8081** (telemetry) - Telemetry endpoint

### Service
- Type: Headless (ClusterIP: None)
- Ports: 8080 (http-metrics), 8081 (telemetry)

### RBAC Permissions
The ClusterRole grants:
- `tokenreviews` and `subjectaccessreviews` (create)
- `customresourcedefinitions` (get, list, watch)
- All resources in all API groups (get, list, watch)

## Custom Resource State Configuration

The `customResourceStateConfig` field contains YAML configuration for monitoring custom resources. Example:

```yaml
kind: CustomResourceStateMetrics
spec:
  resources:
    - groupVersionKind:
        group: account.btp.sap.crossplane.io
        version: "*"
        kind: "*"
      labelsFromPath:
        name: [metadata, name]
        namespace: [metadata, namespace]
      metricNamePrefix: crossplane_btp
      metrics:
        - name: resource_generation
          help: "Resource generation"
          each:
            type: Gauge
            gauge:
              path: [metadata, generation]
```

## Implementation Details

### Controller Structure

The controller implements two main methods:

1. **CreateOrUpdate(ctx, svcobj, config, clusters)**:
   - Called on every add/update event
   - Reconciles all managed resources
   - Returns `ctrl.Result` and error

2. **Delete(ctx, obj, config, clusters)**:
   - Called on delete event
   - Cleans up all managed resources
   - Returns `ctrl.Result` and error

### Helper Functions

- `buildServiceAccount()` - Constructs ServiceAccount object
- `applyServiceAccountSpec()` - Applies spec to ServiceAccount
- `buildClusterRole()` - Constructs ClusterRole object
- `applyClusterRoleSpec()` - Applies spec to ClusterRole
- `buildClusterRoleBinding()` - Constructs ClusterRoleBinding object
- `applyClusterRoleBindingSpec()` - Applies spec to ClusterRoleBinding
- `buildConfigMap()` - Constructs ConfigMap object
- `applyConfigMapSpec()` - Applies spec to ConfigMap
- `buildDeployment()` - Constructs Deployment object
- `applyDeploymentSpec()` - Applies spec to Deployment
- `buildService()` - Constructs Service object
- `applyServiceSpec()` - Applies spec to Service
- `buildLabels()` - Builds standard labels

### Status Management

The controller uses status helpers from `pkg/runtime/status.go`:

- `StatusProgressing(obj, reason, message)` - Sets status to Progressing
- `StatusReady(obj)` - Sets status to Ready
- `StatusTerminating(obj)` - Sets status to Terminating

## Error Handling

- All resource creation failures set status to "Progressing" with error details
- Failed operations return error to trigger reconciliation
- Deployment readiness is checked before setting status to "Ready"
- Requeue with 10-second delay when deployment is not ready or during deletion

## Testing

### E2E Tests

Located in `test/e2e/kubestatemetrics_test.go`

### Test Resources

- `test/e2e/onboarding/kubestatemetrics.yaml` - Sample KubeStateMetrics resource
- `test/e2e/platform/providerconfig.yaml` - Sample ProviderConfig

### Running Tests

```bash
task test-e2e
```

## Examples

See the `examples/` directory:

- `kubestatemetrics-simple.yaml` - Minimal configuration
- `kubestatemetrics-full.yaml` - Full configuration with all options

## Based On

This implementation is based on the POC deployment files from:
```
/Users/i541517/SAPDevelop/I541517/metrics-operator-poc/poc_ksm/ksm/deployment
```

Key files:
- `deployment.yaml` - KSM deployment configuration
- `service.yaml` - Service definition
- `service-account.yaml` - ServiceAccount configuration
- `cluster-role.yaml` - ClusterRole permissions
- `cluster-role-binding.yaml` - ClusterRoleBinding
- `config.yaml` - Custom resource state configuration

## Future Enhancements

Potential improvements:
1. Support for multiple custom resource state configs (via ConfigMap refs)
2. Horizontal Pod Autoscaler support
3. PodDisruptionBudget configuration
4. ServiceMonitor creation for Prometheus Operator
5. Custom metrics exposure configuration
6. Support for namespace-scoped deployments (Role instead of ClusterRole)
7. Webhook validation for spec
