# Development and Testing Scripts

This directory contains scripts for setting up and testing the KubeStateMetrics service provider.

## Prerequisites

1. **OpenMCP Environment**: Use `cluster-provider-kind/hack/local-dev.sh` to set up the base OpenMCP environment:
   ```bash
   cd /path/to/cluster-provider-kind
   ./hack/local-dev.sh deploy
   ```

2. **Build the Service Provider Image**:
   ```bash
   # Build Linux binary
   GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bin/service-provider-ksm-linux ./cmd/service-provider-ksm

   # Build Docker image
   docker build -f Dockerfile.local -t service-provider-ksm:test .
   ```

## Scripts

### `setup-ksm-provider.sh`

Sets up the KubeStateMetrics service provider in an existing OpenMCP environment.

**Usage:**
```bash
./hack/setup-ksm-provider.sh
```

**What it does:**
1. Checks prerequisites (platform, onboarding clusters, Docker image)
2. Loads the service provider image to the platform cluster
3. Installs CRDs on the appropriate clusters:
   - `KubeStateMetrics` and `KubeStateMetricsConfig` CRDs → onboarding cluster
   - `ProviderConfig` CRD → platform cluster
4. Deploys the ServiceProvider resource
5. Waits for initialization (may show warnings, which is expected)
6. Provides next steps for testing

**Environment Variables:**
- `SERVICE_PROVIDER_KSM_IMAGE` (default: `service-provider-ksm:test`)
- `KSM_CRDS_PATH` (default: `../api/crds/manifests`)

## Complete Setup Example

```bash
# 1. Set up OpenMCP environment
cd /path/to/cluster-provider-kind
./hack/local-dev.sh deploy

# This creates:
# - kind-platform cluster (OpenMCP controller)
# - kind-onboarding.* cluster (resource storage)
# - kind-mcp-*.* cluster (managed control plane)

# 2. Build KSM service provider
cd /path/to/service-provider-ksm
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bin/service-provider-ksm-linux ./cmd/service-provider-ksm
docker build -f Dockerfile.local -t service-provider-ksm:test .

# 3. Deploy KSM service provider
./hack/setup-ksm-provider.sh

# 4. Create test resources on onboarding cluster
ONBOARDING_CONTEXT=$(kubectl config get-contexts -o name | grep onboarding | head -1)
kubectl apply -f examples/config.yaml --context $ONBOARDING_CONTEXT
kubectl apply -f examples/kubestatemetrics.yaml --context $ONBOARDING_CONTEXT

# 5. Verify deployment on MCP cluster
MCP_CONTEXT=$(kubectl config get-contexts -o name | grep mcp | head -1)
kubectl get kubestatemetrics --context $ONBOARDING_CONTEXT
kubectl get deploy,svc -n observability --context $MCP_CONTEXT
```

## Testing the Complete Flow

### 1. Check Resources on Onboarding Cluster

```bash
ONBOARDING_CONTEXT=$(kubectl config get-contexts -o name | grep onboarding | head -1)

# Check KubeStateMetricsConfig status
kubectl get kubestatemetricsconfig test-config --context $ONBOARDING_CONTEXT -o yaml

# Check KubeStateMetrics status
kubectl get kubestatemetrics test-ksm --context $ONBOARDING_CONTEXT -o yaml
```

Expected status: `phase: Ready`

### 2. Check Deployment on MCP Cluster

```bash
MCP_CONTEXT=$(kubectl config get-contexts -o name | grep mcp | head -1)

# Check kube-state-metrics deployment
kubectl get deploy kube-state-metrics -n observability --context $MCP_CONTEXT

# Check service
kubectl get svc kube-state-metrics -n observability --context $MCP_CONTEXT

# Check ConfigMap
kubectl get configmap -n observability --context $MCP_CONTEXT
```

Expected:
- Deployment: 1/1 Ready
- Service: ClusterIP (headless)
- ConfigMap: `test-config-ksm-config`

### 3. Check Controller Logs

```bash
# Check service provider controller logs on platform cluster
kubectl logs -n openmcp-system -l app=sp-kubestatemetrics --context kind-platform
```

## Troubleshooting

### ServiceProvider Init Job Fails

**Symptom:** Init job shows error about "no cluster mapping found for label value"

**Explanation:** The init job tries to install CRDs with cluster routing labels, but this feature requires a newer version of mcp-operator.

**Resolution:** This is expected and not a problem because:
- The `setup-ksm-provider.sh` script manually installs CRDs on the correct clusters
- The controller will function normally once the ServiceProvider is deployed
- You can safely ignore init job failures

### Controller Not Starting

**Check ServiceProvider status:**
```bash
kubectl get serviceprovider kubestatemetrics -n openmcp-system -o yaml --context kind-platform
```

**Check for controller deployment:**
```bash
kubectl get deployment -n openmcp-system -l app=sp-kubestatemetrics --context kind-platform
```

### Resources Not Deploying to MCP

**Check AccessRequests:**
```bash
kubectl get accessrequests -A --context kind-platform
```

**Check ClusterRequests:**
```bash
kubectl get clusterrequests -A --context kind-platform
```

**Check controller logs for cluster access errors:**
```bash
kubectl logs -n openmcp-system -l app=sp-kubestatemetrics --context kind-platform | grep -i "cluster\|access"
```

## Cleanup

```bash
# Delete test resources from onboarding cluster
ONBOARDING_CONTEXT=$(kubectl config get-contexts -o name | grep onboarding | head -1)
kubectl delete kubestatemetrics test-ksm --context $ONBOARDING_CONTEXT
kubectl delete kubestatemetricsconfig test-config --context $ONBOARDING_CONTEXT

# Delete ServiceProvider from platform cluster
kubectl delete serviceprovider kubestatemetrics -n openmcp-system --context kind-platform

# Delete entire OpenMCP environment
cd /path/to/cluster-provider-kind
./hack/local-dev.sh reset --force
```

## Additional Resources

- **Documentation**: See `DOCUMENTATION.md` for full API reference and examples
- **Examples**: See `examples/` directory for more configuration examples
- **Testing**: See test scripts in `test/` directory for automated testing
