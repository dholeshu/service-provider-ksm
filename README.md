[![REUSE status](https://api.reuse.software/badge/github.com/openmcp-project/service-provider-template)](https://api.reuse.software/info/github.com/openmcp-project/service-provider-template)

# service-provider-ksm

A Service Provider for managing the lifecycle of [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics) on OpenMCP managed control plane clusters.

## Overview

This service provider automates deployment and management of kube-state-metrics instances across OpenMCP clusters with:

- **Flexible Image Configuration**: Support for any container registry (public, private, SAP internal)
- **Production-Ready**: Zero-downtime updates, PodDisruptionBudget, optimized health probes
- **Configuration Management**: Separate CRD for reusable configurations
- **Full Lifecycle**: Automated create, update, and delete operations
- **Security Hardened**: Non-root, read-only filesystem, minimal RBAC permissions

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

### 2. Create Configuration (Optional)

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

### 3. Deploy kube-state-metrics

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: my-mcp-cluster  # Must match ManagedControlPlane name
  namespace: default
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.13.0
  replicas: 2
  namespace: observability
  configRef:
    name: my-config
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

### 4. Verify Deployment

```shell
# Check resource status on onboarding cluster
kubectl get kubestatemetricsconfig,kubestatemetrics -A

# Check actual deployment on MCP cluster
kubectl get deployment,pods,svc,pdb -n observability --context <mcp-context>
```

## Architecture

### Controller Design

The service provider runs two controllers on the onboarding cluster:

- **KubeStateMetricsConfig controller** — Validates configuration and sets status. Does not require MCP cluster access. Stores the expected ConfigMap name in its status so the KubeStateMetrics controller can use it.
- **KubeStateMetrics controller** — Manages the full kube-state-metrics lifecycle on MCP clusters. Uses `ClusterAccessReconciler` to obtain MCP cluster access via AccessRequests. Creates all resources (Deployment, Service, ConfigMap, RBAC, PDB) on the target MCP cluster.

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
│  ├─ KubeStateMetricsConfig CRD (config validation)           │
│  ├─ KubeStateMetrics CRD (deployment lifecycle)              │
│  └─ User creates resources here                              │
└──────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────┐
│ MCP Cluster (per ManagedControlPlane)                        │
│  ├─ Namespace: observability                                 │
│  ├─ ServiceAccount: kube-state-metrics                       │
│  ├─ ClusterRole: kube-state-metrics (read-only + auth)       │
│  ├─ ClusterRoleBinding: kube-state-metrics                   │
│  ├─ ConfigMap: {config-name}-ksm-config (if config specified)│
│  ├─ Deployment: kube-state-metrics                           │
│  ├─ Service: kube-state-metrics (headless)                   │
│  └─ PodDisruptionBudget: kube-state-metrics                  │
└──────────────────────────────────────────────────────────────┘
```

### Naming Convention

The `KubeStateMetrics` resource name **must match** the `ManagedControlPlane` name. This naming convention is how the `ClusterAccessReconciler` resolves which MCP cluster to deploy to.

```yaml
# KubeStateMetrics on onboarding cluster — name must match MCP name
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: prod-mcp-01  # Must match ManagedControlPlane name
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.13.0
```

### RBAC Permissions

The service provider requests minimal MCP permissions via `ClusterAccessReconciler`:

- **Read-only** (`get`, `list`, `watch`) on all resources — for kube-state-metrics to collect metrics
- **Write** (`create`, `update`, `patch`, `delete`) only for managed resources: namespaces, configmaps, services, serviceaccounts, deployments, poddisruptionbudgets, clusterroles, clusterrolebindings
- **Auth** (`create`) for `tokenreviews` and `subjectaccessreviews` — required by kube-state-metrics for API server authentication

The kube-state-metrics ClusterRole deployed on MCP clusters additionally includes read access to `customresourcedefinitions` for CRD discovery.

## API Reference

### KubeStateMetrics

Manages kube-state-metrics deployment lifecycle on an MCP cluster.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `image` | string | Yes | - | Full image path (e.g., `registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.13.0`) |
| `namespace` | string | No | `observability` | Target namespace on MCP cluster |
| `replicas` | int32 | No | `1` | Number of replicas |
| `configRef` | object | No | - | Reference to KubeStateMetricsConfig |
| `imagePullSecrets` | []LocalObjectReference | No | - | Image pull secrets for private registries |
| `resources` | ResourceRequirements | No | - | CPU/memory requests and limits |
| `args` | []string | No | - | Additional command-line arguments |
| `nodeSelector` | map[string]string | No | - | Node selector for pod scheduling |
| `securityContext` | PodSecurityContext | No | - | Pod-level security context |
| `customResourceStateOnly` | bool | No | `true` | Monitor only custom resources |

### KubeStateMetricsConfig

Defines reusable configuration for kube-state-metrics instances. The config controller validates configuration and stores the expected ConfigMap name in its status. The actual ConfigMap is created on the MCP cluster by the KubeStateMetrics controller, in the same namespace as the deployment.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `targetNamespace` | string | No | Stored in status for reference (default: `observability`). The ConfigMap is deployed to the KubeStateMetrics deployment namespace. |
| `customResourceStateConfig` | string | No | Custom resource state metrics configuration (YAML) |
| `config` | string | No | Standard kube-state-metrics configuration |
| `additionalConfigs` | map[string]string | No | Additional config files (filename -> content) |

### ProviderConfig (Optional)

Global configuration for the service provider, applied on the platform cluster.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `pollInterval` | duration | `1m` | Reconciliation poll interval |
| `defaultVersion` | string | `v2.18.0` | Default kube-state-metrics version (must match `^v\d+\.\d+\.\d+$`) |

## Examples

See the `examples/` directory for complete usage examples including basic deployment, configuration references, shared configs, and standard resource monitoring.

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
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/service-provider-ksm-linux ./cmd/service-provider-ksm/

# Build Docker image for testing
docker build -f Dockerfile.local -t service-provider-ksm:e2e-test .
```

### E2E Testing

The project includes E2E tests using the [openmcp-testing](https://github.com/openmcp-project/openmcp-testing) framework. Tests create a full OpenMCP environment with kind clusters (platform, onboarding, MCP) and verify the service provider end-to-end.

```shell
# Build and run E2E tests
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/service-provider-ksm-linux ./cmd/service-provider-ksm/
docker build -f Dockerfile.local -t service-provider-ksm:e2e-test .
go test -v ./test/e2e/... -timeout 30m
```

The E2E test:
1. Creates platform, onboarding, and MCP kind clusters
2. Deploys the OpenMCP operator and cluster/service providers
3. Creates `KubeStateMetricsConfig` and `KubeStateMetrics` resources on the onboarding cluster
4. Verifies both resources reach `Ready` status
5. Tears down resources and clusters

Test fixtures are in `test/e2e/onboarding/` (resources applied to onboarding cluster) and `test/e2e/platform/` (ProviderConfig applied to platform cluster).

### Code Generation

After modifying API types:

```shell
task generate:all
```

## Troubleshooting

### Pod Not Starting (ImagePullBackOff)

Ensure the image exists and is accessible. If using a private registry, create an imagePullSecret on the MCP cluster and reference it in the `KubeStateMetrics` spec.

### Resource Stuck in Progressing

1. Check controller logs: `kubectl logs -n openmcp-system deployment/sp-kubestatemetrics --context kind-platform`
2. Check AccessRequest status: `kubectl get accessrequest -A --context kind-platform`
3. Check deployment on MCP: `kubectl get deployment kube-state-metrics -n observability --context <mcp-context>`

### KubeStateMetricsConfig Not Becoming Ready

The config controller validates configuration on the onboarding cluster. If it's not becoming Ready, check the controller logs for validation errors.

### Wrong MCP Cluster

Ensure the `KubeStateMetrics` resource name matches the `ManagedControlPlane` name exactly.

## Contributing

This project is open to feature requests, bug reports, and contributions via [GitHub issues](https://github.com/dholeshu/service-provider-ksm/issues).

## Security

If you find a security issue, please follow the instructions in our [security policy](https://github.com/openmcp-project/service-provider-template/security/policy).

## Code of Conduct

We pledge to make participation in our community harassment-free. By participating, you agree to abide by the [Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md).

## License

Copyright 2025 SAP SE or an SAP affiliate company and service-provider-ksm contributors.

Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/openmcp-project/service-provider-template).
