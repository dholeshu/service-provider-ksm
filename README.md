[![REUSE status](https://api.reuse.software/badge/github.com/openmcp-project/service-provider-template)](https://api.reuse.software/info/github.com/openmcp-project/service-provider-template)

# service-provider-ksm

A Service Provider for managing the lifecycle of [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics) on OpenMCP managed control plane clusters.

## Overview

This service provider automates deployment and management of kube-state-metrics instances across OpenMCP clusters with:

- **Flexible Image Configuration**: Support for any container registry (public, private, SAP internal)
- **Production-Ready**: Zero-downtime updates, PodDisruptionBudget, optimized health probes
- **MCP-Native Configuration**: Deploy a ConfigMap directly on the MCP cluster — no extra CRDs needed
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

### 2. Deploy kube-state-metrics

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

### 3. (Optional) Provide Configuration via MCP-Native ConfigMap

Create a ConfigMap named `kube-state-metrics-config` directly on the MCP cluster:

```yaml
# Apply this to the MCP cluster
apiVersion: v1
kind: ConfigMap
metadata:
  name: kube-state-metrics-config
  namespace: observability
data:
  custom-resource-state-config.yaml: |
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

**Key behavior:**
- **Fixed name**: Must be `kube-state-metrics-config` in the KSM deployment namespace (default: `observability`)
- **Auto-restart**: When ConfigMap data changes, pods automatically restart via rolling update (SHA-256 hash annotation)
- **Ownership**: The controller never deletes user-created ConfigMaps

### 4. Verify Deployment

```shell
# Check resource status on onboarding cluster
kubectl get kubestatemetrics -A

# Check actual deployment on MCP cluster
kubectl get deployment,pods,svc,pdb -n observability --context <mcp-context>
```

## Architecture

### Controller Design

The service provider runs on the onboarding cluster:

- **KubeStateMetrics controller** — Manages the full kube-state-metrics lifecycle on MCP clusters. Uses `ClusterAccessReconciler` to obtain MCP cluster access via AccessRequests. Creates all resources (Deployment, Service, RBAC, PDB) on the target MCP cluster. Auto-detects an MCP-native ConfigMap and triggers rolling restarts on config changes.

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
│  ├─ ConfigMap: kube-state-metrics-config (user-created)      │
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
| `imagePullSecrets` | []LocalObjectReference | No | - | Image pull secrets for private registries |
| `resources` | ResourceRequirements | No | - | CPU/memory requests and limits |
| `args` | []string | No | - | Additional command-line arguments |
| `nodeSelector` | map[string]string | No | - | Node selector for pod scheduling |
| `securityContext` | PodSecurityContext | No | - | Pod-level security context |

> **Auto-detection:** If a ConfigMap named `kube-state-metrics-config` exists in the deployment namespace, the controller inspects its keys and auto-derives KSM flags:
> - `custom-resource-state-config.yaml` present → adds `--custom-resource-state-config-file`
> - `custom-resource-state-config.yaml` present **without** `config.yaml` → also adds `--custom-resource-state-only`
> - `config.yaml` present → adds `--config`

### ProviderConfig (Optional)

Global configuration for the service provider, applied on the platform cluster.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `pollInterval` | duration | `1m` | Reconciliation poll interval |
| `defaultVersion` | string | `v2.18.0` | Default kube-state-metrics version (must match `^v\d+\.\d+\.\d+$`) |

## Examples

See the `examples/` directory for complete usage examples including basic deployment and MCP-native ConfigMap configuration.

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
# Build image and run E2E tests
task test-e2e
```

The E2E suite covers four ConfigMap scenarios:

| Test | ConfigMap Keys | Verified Args | Volume |
|------|---------------|--------------|--------|
| `TestNoConfigMap` | none | none | no |
| `TestCRSConfigOnly` | `custom-resource-state-config.yaml` | `--custom-resource-state-config-file` + `--custom-resource-state-only` | yes |
| `TestStdConfigOnly` | `config.yaml` | `--config` | yes |
| `TestBothConfigs` | both | `--custom-resource-state-config-file` + `--config` (no `--custom-resource-state-only`) | yes |

Each test creates its own MCP cluster lifecycle, deploys KSM, optionally creates a ConfigMap programmatically, and verifies the resulting deployment args and volume mounts.

Test fixtures are in `test/e2e/onboarding/` (KubeStateMetrics resource) and `test/e2e/platform/` (ProviderConfig).

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
