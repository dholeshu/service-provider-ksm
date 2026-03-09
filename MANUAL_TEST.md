# Manual Testing Guide for service-provider-ksm (MCP Architecture)

This guide walks you through manually testing the service-provider-ksm running ON MCP clusters.

## Architecture Overview

**NEW Architecture (as of redesign):**
- Service provider runs **ON each MCP cluster** (not on platform or onboarding)
- Users create KubeStateMetrics and KubeStateMetricsConfig resources **on their MCP cluster**
- Service provider watches its local MCP cluster and deploys kube-state-metrics locally
- No cross-cluster watching needed - everything happens locally

## Prerequisites

1. **OpenMCP Platform cluster** with:
   - OpenMCP operator installed and running
   - Cluster provider (e.g., kind cluster provider) installed
   - Access to the platform cluster via kubectl

2. **Build tools**:
   - Docker
   - kubectl
   - kind (if using kind clusters)
   - Go 1.23+

## Step 1: Build and Load the Service Provider Image

```bash
# Build the service provider binary for Linux
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/service-provider-ksm ./cmd/service-provider-ksm

# Build Docker image
docker build -t service-provider-ksm:test -f Dockerfile .

# Load image into your platform cluster (if using kind)
kind load docker-image service-provider-ksm:test --name platform
```

## Step 2: Create an MCP Cluster

Create an MCP cluster where the service provider will run:

```bash
kubectl config use-context kind-platform

cat <<EOF | kubectl apply -f -
apiVersion: clusters.openmcp.cloud/v1alpha1
kind: Cluster
metadata:
  name: test-mcp
  namespace: openmcp-system
spec:
  purposes:
    - mcp
  profile: kind
  tenancy: Shared
  kubernetes: {}
EOF
```

Wait for the cluster to become Ready:
```bash
kubectl get cluster test-mcp -n openmcp-system -w
```

This will create a new kind cluster. Verify it exists:
```bash
kind get clusters
```

You should see something like `test-mcp.<hash>`.

## Step 3: Deploy ServiceProvider to MCP Clusters

Create a ServiceProvider resource on the platform cluster. OpenMCP will automatically deploy it to all MCP clusters:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: openmcp.cloud/v1alpha1
kind: ServiceProvider
metadata:
  name: kubestatemetrics
spec:
  image: service-provider-ksm:test
  runReplicas: 1
  verbosity: INFO
  # TODO: Verify if deploymentMode or similar is needed to deploy to MCP clusters
EOF
```

**Note**: You may need to configure the ServiceProvider to deploy to MCP clusters. Check OpenMCP documentation for the correct spec fields.

Wait for the service provider to become Ready:
```bash
kubectl get serviceprovider kubestatemetrics -w
```

## Step 4: Verify Service Provider is Running on MCP Cluster

Switch to the MCP cluster context:
```bash
# Find the MCP cluster name
kubectl get cluster test-mcp -n openmcp-system

# Switch context (replace with actual cluster name)
kubectl config use-context kind-test-mcp.<hash>
```

Check if service provider is running:
```bash
kubectl get pods -A | grep kubestatemetrics

# Should see:
# - sp-kubestatemetrics-init-* (Completed) - CRD installation
# - sp-kubestatemetrics-* (Running) - Controller
```

Check the CRDs are installed:
```bash
kubectl get crds | grep ksm
```

Expected output:
```
kubestatemetrics.ksm.services.openmcp.cloud
kubestatemetricsconfigs.ksm.services.openmcp.cloud
```

Check logs:
```bash
kubectl logs -n <namespace> -l app=sp-kubestatemetrics --tail=50
```

## Step 5: Create Test Resources on MCP Cluster

While still on the MCP cluster context, create the test resources:

```bash
kubectl apply -f test/e2e/mcp/kubestatemetrics.yaml
```

Verify resources were created:
```bash
kubectl get kubestatemetricsconfig,kubestatemetrics
```

## Step 6: Verify Deployment

### Check KubeStateMetricsConfig Status

```bash
kubectl get kubestatemetricsconfig test-config -o yaml
```

Look for:
```yaml
status:
  phase: Ready
  configMapName: test-config-ksm-config
  configMapNamespace: default
```

### Check ConfigMap Creation

```bash
kubectl get configmap test-config-ksm-config -n default -o yaml
```

### Check KubeStateMetrics Status

```bash
kubectl get kubestatemetrics test-ksm -o yaml
```

Look for:
```yaml
status:
  phase: Ready
```

### Check Deployed Resources

```bash
# Check namespace
kubectl get namespace observability

# Check all resources
kubectl get all -n observability
```

You should see:
- ServiceAccount: `kube-state-metrics`
- ClusterRole: `kube-state-metrics`
- ClusterRoleBinding: `kube-state-metrics`
- Deployment: `kube-state-metrics`
- Service: `kube-state-metrics`

### Test the Metrics Endpoint

Check if kube-state-metrics pod is running:
```bash
kubectl get pods -n observability
```

Port-forward to access metrics:
```bash
kubectl port-forward -n observability svc/kube-state-metrics 8080:8080
```

In another terminal, fetch metrics:
```bash
curl http://localhost:8080/metrics | grep crossplane_btp
```

You should see custom metrics based on your configuration.

## Step 7: Test Updates

### Update the Configuration

```bash
kubectl edit kubestatemetricsconfig test-config
```

Add or modify the `customResourceStateConfig` and save.

Check if the ConfigMap gets updated:
```bash
kubectl get configmap test-config-ksm-config -n default -o yaml
```

The deployment should automatically reload with the new config.

### Scale the Deployment

```bash
kubectl edit kubestatemetrics test-ksm
```

Change `replicas` from 1 to 2 and save.

Verify:
```bash
kubectl get deployment kube-state-metrics -n observability
```

## Step 8: Test Deletion

### Delete KubeStateMetrics

```bash
kubectl delete kubestatemetrics test-ksm
```

Verify resources are removed:
```bash
kubectl get all -n observability
```

The deployment, service, and related resources should be deleted.

### Delete KubeStateMetricsConfig

```bash
kubectl delete kubestatemetricsconfig test-config
```

Verify ConfigMap is removed:
```bash
kubectl get configmap -n default | grep ksm-config
```

## Troubleshooting

### Service Provider Not Starting on MCP

1. Check if ServiceProvider was deployed to MCP:
```bash
kubectl get pods -A | grep kubestatemetrics
```

2. Check init job logs:
```bash
kubectl logs -n <namespace> sp-kubestatemetrics-init-<pod-id>
```

3. Check if CRDs are installed:
```bash
kubectl get crds | grep ksm
```

### Resources Not Deploying

1. Check service provider logs for errors:
```bash
kubectl logs -n <namespace> -l app=sp-kubestatemetrics --tail=100
```

2. Verify KubeStateMetricsConfig has `status.configMapName` set
3. Check resource status for error messages

### "No CRD found" Errors

The CRDs might not be installed. Check:
```bash
kubectl get crds | grep ksm
```

If missing, the init job may have failed. Check its logs.

## Clean Up

To clean up everything:

```bash
# On MCP cluster
kubectl delete kubestatemetrics --all
kubectl delete kubestatemetricsconfig --all

# Switch to platform cluster
kubectl config use-context kind-platform

# Delete ServiceProvider
kubectl delete serviceprovider kubestatemetrics

# Delete MCP cluster
kubectl delete cluster test-mcp -n openmcp-system

# Delete the kind cluster
kind delete cluster --name test-mcp.<hash>
```

## Key Differences from Old Architecture

**Old Architecture (Onboarding-based):**
- Resources created on onboarding cluster
- Service provider watched onboarding cluster
- Deployed to remote MCP clusters via ClusterAccess

**New Architecture (MCP-local):**
- Resources created on MCP cluster
- Service provider runs ON MCP cluster
- Deploys to local cluster (simple!)
- No complex cross-cluster watching
- Users only need MCP cluster access

## Notes

- The service provider must be configured to deploy to MCP clusters in the ServiceProvider spec
- Each MCP cluster gets its own service provider instance
- Resources are completely local to each MCP cluster
- No platform or onboarding cluster access needed for end users
