[![REUSE status](https://api.reuse.software/badge/github.com/openmcp-project/service-provider-template)](https://api.reuse.software/info/github.com/openmcp-project/service-provider-template)

# service-provider-ksm

A Service Provider for managing the lifecycle of [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics) on OpenMCP managed control plane clusters.

## Overview

This service provider automates deployment and management of kube-state-metrics instances across OpenMCP clusters with:

- **Flexible Image Configuration**: Support for any container registry (public, private, SAP internal)
- **Production-Ready**: Zero-downtime updates, PodDisruptionBudget, optimized health probes
- **Configuration Management**: Separate CRD for reusable configurations
- **Full Lifecycle**: Automated create, update, and delete operations
- **Security Hardened**: Non-root, read-only filesystem, minimal permissions

## Quick Start

### 1. Install the Service Provider

```shell
# Deploy ServiceProvider on platform cluster
kubectl apply -f - <<EOF
apiVersion: openmcp.cloud/v1alpha1
kind: ServiceProvider
metadata:
  name: kubestatemetrics
spec:
  image: ghcr.io/dholeshu/service-provider-ksm:latest
  runReplicas: 1
  verbosity: INFO
EOF
```

### 2. Create Image Pull Secret (if needed)

```shell
# On each MCP cluster where kube-state-metrics will be deployed
# Example for private registry
kubectl create secret docker-registry my-registry-secret \
  --docker-server=<your-registry> \
  --docker-username=<username> \
  --docker-password=<token> \
  -n observability
```

### 3. Create Configuration (Optional)

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: my-config
  namespace: default
spec:
  targetNamespace: observability
  customResourceStateConfig: |
    spec:
      resources:
        - groupVersionKind:
            group: "apps"
            version: "v1"
            kind: "Deployment"
          metricNamePrefix: kube_deployment
          metrics:
            - name: "replicas"
              help: "Number of replicas"
              each:
                type: Gauge
                gauge:
                  path: [spec, replicas]
```

### 4. Deploy kube-state-metrics

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: my-mcp-cluster  # Must match ManagedControlPlane name
  namespace: default
spec:
  # Full image path including registry, repository, and tag
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0

  replicas: 2  # Recommended for HA
  namespace: observability

  configRef:
    name: my-config

  # Image pull secrets (if using private registry)
  imagePullSecrets:
    - name: my-registry-secret

  args:
    - --resources=deployments,pods,nodes,services

  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi
```

### 5. Verify Deployment

```shell
# Check resource status on onboarding cluster
kubectl get kubestatemetricsconfig,kubestatemetrics -A

# Check actual deployment on MCP cluster
kubectl get deployment,pods,svc,pdb -n observability --context <mcp-context>

# Test metrics endpoint
kubectl run curl-test --image=curlimages/curl:latest --rm -i --restart=Never \
  -n observability --context <mcp-context> -- \
  curl -s http://kube-state-metrics:8080/metrics
```

## Features

### Flexible Image Configuration

- **Any Registry**: Use public registries (registry.k8s.io, docker.io), private registries, or internal registries
- **Full Control**: Specify complete image path including registry, repository, and tag
- **Version Pinning**: Pin to specific versions or use image digests
- **Custom Builds**: Support for custom kube-state-metrics builds

**Examples:**
```yaml
# Public registry
image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0

# Docker Hub
image: docker.io/kubernetes/kube-state-metrics:v2.18.0

# Private/corporate registry
image: my-registry.company.com/kube-state-metrics/kube-state-metrics:v2.18.0

# Image digest for immutability
image: registry.k8s.io/kube-state-metrics/kube-state-metrics@sha256:abc123...
```

### Production-Ready Deployment

- **Zero-Downtime Updates**: `maxUnavailable: 0` for multi-replica deployments
- **Rolling Update Strategy**: `maxSurge: 1` allows one extra pod during rollout
- **Stabilization Period**: `minReadySeconds: 10` ensures pods are stable
- **PodDisruptionBudget**: `minAvailable: 1` protects against voluntary disruptions
- **Optimized Probes**: Realistic delays and failure thresholds
- **Graceful Termination**: 30-second grace period for clean shutdown

### Configuration Management

- **Separate CRD**: `KubeStateMetricsConfig` for reusable configurations
- **ConfigMap Creation**: Automatically creates and manages ConfigMaps on MCP clusters
- **Custom Resources**: Define custom resource state metrics
- **Shared Configs**: One config can be used by multiple KSM instances

### Security

- **Non-root User**: Runs as UID 65534
- **Read-only Filesystem**: Container filesystem is read-only
- **Dropped Capabilities**: All Linux capabilities dropped
- **Security Context**: Seccomp profile and privilege escalation prevention
- **RBAC**: Minimal cluster-scoped read-only permissions

## Architecture

### OpenMCP Integration

```
┌──────────────────────────────────────────────────────────────┐
│ Platform Cluster                                             │
│  ├─ ServiceProvider: kubestatemetrics                        │
│  ├─ ProviderConfig CRD (optional defaults)                   │
│  └─ ClusterRequests → MCP clusters                           │
└──────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────┐
│ Onboarding Cluster                                           │
│  ├─ Controller Pod: sp-kubestatemetrics                      │
│  ├─ KubeStateMetricsConfig CRD                               │
│  ├─ KubeStateMetrics CRD                                     │
│  └─ User creates resources here                              │
└──────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────┐
│ MCP Cluster (mcp-xxxxx)                                      │
│  ├─ Namespace: observability                                 │
│  ├─ ServiceAccount: kube-state-metrics                       │
│  ├─ ClusterRole: kube-state-metrics (read-only)              │
│  ├─ ClusterRoleBinding: kube-state-metrics                   │
│  ├─ ConfigMap: {name}-ksm-config (if config specified)       │
│  ├─ Deployment: kube-state-metrics                           │
│  ├─ Service: kube-state-metrics (headless)                   │
│  └─ PodDisruptionBudget: kube-state-metrics                  │
└──────────────────────────────────────────────────────────────┘
```

### Naming Convention

**Critical**: The `KubeStateMetrics` resource name **must match** the `ManagedControlPlane` name.

```yaml
# ManagedControlPlane on platform cluster
apiVersion: core.openmcp.cloud/v1alpha1
kind: ManagedControlPlane
metadata:
  name: prod-mcp-01  # This name is important

---
# KubeStateMetrics on onboarding cluster
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: prod-mcp-01  # Must match MCP name
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0
```

This naming convention ensures resources are deployed to the correct MCP cluster.

## API Reference

### KubeStateMetrics

Manages kube-state-metrics deployment lifecycle.

#### Spec Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `image` | string | Yes | - | Full image path (e.g., `registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0`) |
| `namespace` | string | No | `observability` | Target namespace on MCP cluster |
| `replicas` | int32 | No | `1` | Number of replicas (2+ recommended for HA) |
| `configRef` | object | No | - | Reference to KubeStateMetricsConfig |
| `imagePullSecrets` | []LocalObjectReference | No | - | Image pull secrets for private registries |
| `resources` | ResourceRequirements | No | - | CPU/memory requests and limits |
| `args` | []string | No | - | Additional command-line arguments |
| `nodeSelector` | map[string]string | No | - | Node selector for pod scheduling |
| `securityContext` | PodSecurityContext | No | - | Pod-level security context |
| `customResourceStateOnly` | bool | No | `true` | Monitor only custom resources |

#### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Current phase: `Ready`, `Progressing`, or `Error` |
| `conditions` | []Condition | Detailed status conditions |
| `observedGeneration` | int64 | Last reconciled generation |

#### Example

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: prod-cluster
  namespace: default
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0
  replicas: 2
  namespace: observability
  configRef:
    name: prod-config
  imagePullSecrets:
    - name: my-registry-secret
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi
  nodeSelector:
    workload-type: monitoring
```

### KubeStateMetricsConfig

Manages configuration files for kube-state-metrics.

#### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `targetNamespace` | string | No | Namespace where ConfigMap will be created (default: `observability`) |
| `customResourceStateConfig` | string | No | Custom resource state metrics configuration (YAML) |
| `config` | string | No | Standard kube-state-metrics configuration |
| `additionalConfigs` | map[string]string | No | Additional config files (filename → content) |

**At least one of the config fields must be provided.**

#### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Current phase: `Ready` or `Error` |
| `configMapName` | string | Name of created ConfigMap |
| `configMapNamespace` | string | Namespace of created ConfigMap |
| `observedGeneration` | int64 | Last reconciled generation |

#### Example

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: prod-config
  namespace: default
spec:
  targetNamespace: observability
  customResourceStateConfig: |
    spec:
      resources:
        - groupVersionKind:
            group: "apps"
            version: "v1"
            kind: "Deployment"
          metricNamePrefix: kube_deployment
          metrics:
            - name: "replicas"
              help: "Number of desired replicas"
              each:
                type: Gauge
                gauge:
                  path: [spec, replicas]
            - name: "available_replicas"
              help: "Number of available replicas"
              each:
                type: Gauge
                gauge:
                  path: [status, availableReplicas]
```

### ProviderConfig (Optional)

Global configuration for the service provider.

#### Spec Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `pollInterval` | duration | `1m` | Reconciliation poll interval |

#### Example

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: ProviderConfig
metadata:
  name: default
spec:
  pollInterval: 1m
```

## Production Deployment Guide

### High Availability Setup

For production workloads, deploy with multiple replicas:

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: prod-mcp
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0
  replicas: 3  # Multiple replicas for HA
  namespace: observability

  # Anti-affinity to spread across nodes
  affinity:
    podAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 100
          podAffinityTerm:
            labelSelector:
              matchLabels:
                app.kubernetes.io/name: kube-state-metrics
            topologyKey: kubernetes.io/hostname

  # Resource limits for production
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
    limits:
      cpu: 1000m
      memory: 1Gi
```

**Production Checklist:**

- ✅ Deploy 2+ replicas for zero-downtime updates
- ✅ Configure PodDisruptionBudget (automatically created)
- ✅ Set appropriate resource requests/limits
- ✅ Use anti-affinity to spread across nodes
- ✅ Configure imagePullSecret for private registries
- ✅ Monitor deployment status and metrics
- ✅ Set up alerts for pod failures

### Zero-Downtime Updates

With `replicas: 2+`, the deployment strategy ensures:

1. **New pod created** (surge to N+1 total)
2. **New pod becomes ready** (probes pass + minReadySeconds)
3. **Old pod #1 terminates** gracefully
4. **Repeat** for remaining replicas
5. **Result**: Zero downtime ✅

For single-replica deployments:
- Brief downtime (~5-10 seconds) during updates
- Acceptable for non-critical monitoring
- Scale to 2+ replicas for zero-downtime

### Version Upgrades

To upgrade kube-state-metrics version:

```shell
# Update the image field
kubectl patch kubestatemetrics prod-mcp --type=merge -p '{"spec":{"image":"registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.19.0"}}'

# Watch the rollout
kubectl get kubestatemetrics prod-mcp -w

# Verify on MCP cluster
kubectl rollout status deployment/kube-state-metrics -n observability --context <mcp-context>
```

The controller will:
1. Update the Deployment with new image
2. Kubernetes performs rolling update
3. New pods start with new version
4. Old pods terminate gracefully
5. Status updates to `Ready` when complete

## Examples

See the `examples/` directory for complete examples:

- **`kubestatemetrics.yaml`** - Basic deployment
- **`with-config-ref.yaml`** - Using configuration reference
- **`shared-config.yaml`** - Multiple deployments sharing one config
- **`standard-resources.yaml`** - Monitoring standard Kubernetes resources
- **`with-full-config.yaml`** - Complete configuration with all options

## Development

### Prerequisites

- Go 1.23+
- Docker
- kind (for local testing)
- kubectl
- Task (https://taskfile.dev)

### Building

```shell
# Build binary
task build:bin:build-raw

# Build Docker image
docker build -f Dockerfile.local -t service-provider-ksm:dev .
```

### Testing Locally

```shell
# Build and load image
docker build -f Dockerfile.local -t service-provider-ksm:local-test .
kind load docker-image service-provider-ksm:local-test --name platform

# Deploy ServiceProvider and test resources
kubectl apply -f test/local/local-test.yaml --context kind-platform

# Check logs
kubectl logs -n openmcp-system deployment/sp-kubestatemetrics --context kind-platform -f
```

See [`test/local/README.md`](test/local/README.md) for detailed testing instructions.

### Code Generation

After modifying API types:

```shell
# Regenerate code and manifests
task generate:all

# Or individually
task generate:code       # DeepCopy methods
task generate:manifests  # CRD manifests
task generate:format     # Code formatting
```

## Troubleshooting

### Pod Not Starting (ImagePullBackOff)

**Symptom**: Pod stuck in `ImagePullBackOff` or `ErrImagePull`

**Solution**: Ensure imagePullSecret exists (if using private registry):

```shell
# Check secret exists on MCP cluster
kubectl get secret my-registry-secret -n observability --context <mcp-context>

# Verify it's referenced in KubeStateMetrics spec
kubectl get kubestatemetrics <name> -o jsonpath='{.spec.imagePullSecrets}'

# Test credentials manually
docker login <your-registry> -u <username>
```

### Resource Stuck in Progressing

**Symptom**: KubeStateMetrics stays in `Progressing` phase

**Possible Causes:**
1. AccessRequest not granted yet (wait for ClusterProvider)
2. Deployment not ready (check pod status on MCP)
3. ConfigMap not created (check KubeStateMetricsConfig status)

**Debug Steps:**

```shell
# Check AccessRequest status
kubectl get accessrequest -A --context kind-platform | grep <name>

# Check deployment on MCP
kubectl get deployment kube-state-metrics -n observability --context <mcp-context>

# Check pod logs
kubectl logs -n observability deployment/kube-state-metrics --context <mcp-context>

# Check controller logs
kubectl logs -n openmcp-system deployment/sp-kubestatemetrics --context kind-platform --tail=50
```

### ConfigMap Not Found

**Symptom**: Deployment exists but pods crash with "ConfigMap not found"

**Solution**: Ensure KubeStateMetricsConfig is created first:

```shell
# Check config status
kubectl get kubestatemetricsconfig <name> -o yaml

# Verify ConfigMap was created on MCP
kubectl get configmap -n observability --context <mcp-context>

# Check if names match
kubectl get kubestatemetrics <name> -o jsonpath='{.spec.configRef.name}'
```

### Wrong MCP Cluster

**Symptom**: Resources deployed to wrong cluster

**Cause**: Name mismatch between KubeStateMetrics and ManagedControlPlane

**Solution**: Ensure names match exactly:

```shell
# List ManagedControlPlanes
kubectl get managedcontrolplane -A --context kind-platform

# Ensure KubeStateMetrics name matches
kubectl get kubestatemetrics -A
```

## ManagedControlPlane Integration

For automatic KubeStateMetrics deployment via ManagedControlPlane spec, additional changes are required in mcp-operator. See [REQUIRED_CHANGES_MCP_OPERATOR.md](REQUIRED_CHANGES_MCP_OPERATOR.md) for the complete specification.

**Current Status**: Direct resource creation is fully supported. ManagedControlPlane integration requires mcp-operator changes.

## Runtime Configuration

The service provider supports these flags:

- `--verbosity`: Logging verbosity level (see [controller-runtime logging](https://github.com/kubernetes-sigs/controller-runtime/blob/main/TMP-LOGGING.md))
- `--environment`: Environment name (required)
- `--provider-name`: Provider resource name (required)
- `--metrics-bind-address`: Metrics endpoint address (default: `0`)
- `--health-probe-bind-address`: Health probe address (default: `:8081`)
- `--leader-elect`: Enable leader election (default: `false`)
- `--metrics-secure`: Serve metrics via HTTPS (default: `true`)
- `--enable-http2`: Enable HTTP/2 (default: `false`)

Run `--help` for complete list.

## Contributing

This project is open to feature requests, bug reports, and contributions via [GitHub issues](https://github.com/dholeshu/service-provider-ksm/issues).

## Security

If you find a security issue, please follow the instructions in our [security policy](https://github.com/openmcp-project/service-provider-template/security/policy).

## Code of Conduct

We pledge to make participation in our community harassment-free. By participating, you agree to abide by the [Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md).

## License

Copyright 2025 SAP SE or an SAP affiliate company and service-provider-ksm contributors.

Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/openmcp-project/service-provider-template).
