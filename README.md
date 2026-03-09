[![REUSE status](https://api.reuse.software/badge/github.com/openmcp-project/service-provider-template)](https://api.reuse.software/info/github.com/openmcp-project/service-provider-template)

# service-provider-ksm

## About this project

A Service Provider for managing the lifecycle of [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics) on OpenMCP clusters.

This service provider automates the deployment and management of kube-state-metrics instances, including:
- ServiceAccount, ClusterRole, and ClusterRoleBinding setup
- ConfigMap management for custom resource state configuration
- Deployment with security best practices
- Service exposure for metrics endpoints
- Full lifecycle management (create, update, delete)

## Quick Start

### 1. Create a Configuration

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: my-config
  namespace: observability
spec:
  customResourceStateConfig: |
    kind: CustomResourceStateMetrics
    spec:
      resources:
        - groupVersionKind:
            group: apps
            version: "*"
            kind: "Deployment"
          metricNamePrefix: app
          metrics:
            - name: deployment_replicas
              help: "Number of desired replicas"
              each:
                type: Gauge
                gauge:
                  path: [spec, replicas]
```

### 2. Create a KubeStateMetrics Resource

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: my-ksm
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0
  namespace: observability
  customResourceStateOnly: true
  configRef:
    name: my-config
    namespace: observability
```

### 3. Apply the Resources

```shell
kubectl apply -f my-config.yaml
kubectl apply -f my-ksm.yaml
```

### 4. Check Status

```shell
kubectl get kubestatemetricsconfig my-config -o yaml
kubectl get kubestatemetrics my-ksm -o yaml
```

See `examples/` directory for more configuration examples.

## Features

- ✅ **Separate Configuration CRD** - Manage configuration independently with `KubeStateMetricsConfig`
- ✅ **Configuration Reusability** - Share configs across multiple KSM deployments
- ✅ **Full Lifecycle Management** - Creates, updates, and deletes all required Kubernetes resources
- ✅ **Custom Resource Monitoring** - Configurable custom resource state metrics
- ✅ **Standard Resource Monitoring** - Support for built-in Kubernetes resources
- ✅ **Security Hardened** - Follows security best practices (non-root, read-only filesystem, etc.)
- ✅ **Flexible Configuration** - Supports resource limits, node selectors, image pull secrets
- ✅ **Status Reporting** - Reports deployment status with conditions and phases

## Generated Resources

This service provider was generated from the [service-provider-template](https://github.com/openmcp-project/service-provider-template) with the following parameters:

- **Module**: `github.com/dholeshu/service-provider-ksm`
- **Kind**: `KubeStateMetrics`
- **Group**: `ksm`
- **API Group**: `ksm.services.openmcp.cloud/v1alpha1`

## Building

Build the service provider binary:

```shell
go build -o bin/service-provider-ksm ./cmd/service-provider-ksm
```

## Running Tests

Run end-to-end tests:

```shell
task test-e2e
```

## Development

After modifying the API types in `api/v1alpha1/kubestatemetrics_types.go`, regenerate code and manifests:

```shell
task generate
```

## Documentation

- **[CONFIG_MANAGEMENT.md](CONFIG_MANAGEMENT.md)** - Configuration management with KubeStateMetricsConfig
- **[SETUP.md](SETUP.md)** - Detailed setup guide and development workflow
- **[IMPLEMENTATION.md](IMPLEMENTATION.md)** - Implementation details and architecture
- **[examples/](examples/)** - Example resources with different configurations

## API Reference

### KubeStateMetricsConfig

Manages configuration files for kube-state-metrics.

**Required Fields:**
- At least one of: `spec.customResourceStateConfig`, `spec.config`, or `spec.additionalConfigs`

**Optional Fields:**
- `spec.customResourceStateConfig` - Custom resource state metrics configuration
- `spec.config` - Standard kube-state-metrics configuration
- `spec.additionalConfigs` - Additional configuration files (map of filename -> content)

### KubeStateMetrics

Manages kube-state-metrics deployment.

**Required Fields:**
- `spec.image` - Container image to use (e.g., "registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0")
- `spec.configRef` - Reference to a KubeStateMetricsConfig resource

**Optional Fields:**

- `spec.namespace` - Target namespace (default: "observability")
- `spec.replicas` - Number of replicas (default: 1)
- `spec.customResourceStateOnly` - Monitor only custom resources (default: true)
- `spec.customResourceStateConfig` - Custom resource state metrics configuration
- `spec.imagePullSecrets` - Image pull secrets for private registries
- `spec.resources` - Resource requests and limits
- `spec.args` - Additional command-line arguments
- `spec.nodeSelector` - Node selector for pod scheduling
- `spec.securityContext` - Pod security context

### Status Fields

- `status.phase` - Current phase: Ready, Progressing, or Terminating
- `status.conditions` - Detailed conditions
- `status.observedGeneration` - Last reconciled generation

## Runtime Configuration

The service provider supports the following runtime flags:

- `--verbosity`: Logging verbosity level (see [controller-runtime logging](https://github.com/kubernetes-sigs/controller-runtime/blob/main/TMP-LOGGING.md))
- `--environment`: Name of the environment (required for operation)
- `--provider-name`: Name of the provider resource (required for operation)
- `--metrics-bind-address`: Address for the metrics endpoint (default: `0`, use `:8443` for HTTPS or `:8080` for HTTP)
- `--health-probe-bind-address`: Address for health probe endpoint (default: `:8081`)
- `--leader-elect`: Enable leader election for controller manager (default: `false`)
- `--metrics-secure`: Serve metrics endpoint securely via HTTPS (default: `true`)
- `--enable-http2`: Enable HTTP/2 for metrics and webhook servers (default: `false`)

For a complete list of available flags, run the generated binary with `-h` or `--help`.

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/openmcp-project/service-provider-template/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure

If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/openmcp-project/service-provider-template/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright 2025 SAP SE or an SAP affiliate company and service-provider-template contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/openmcp-project/service-provider-template).
