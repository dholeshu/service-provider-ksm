# Targeting MCP Clusters - Naming Convention

## Overview

The service-provider-ksm uses a **naming convention** to determine which MCP cluster to deploy resources to. This follows the same pattern as service-provider-crossplane and other OpenMCP service providers.

## The Naming Convention

**The service resource name MUST match the ManagedControlPlaneV2 name.**

This is an implicit API contract enforced by the `ClusterAccessReconciler` library.

## How It Works

### 1. User Creates ManagedControlPlaneV2

```yaml
apiVersion: core.openmcp.cloud/v1alpha1
kind: ManagedControlPlaneV2
metadata:
  name: monitoring-cluster  # ← This name matters!
  namespace: default
spec:
  # MCP configuration
```

### 2. MCP Operator Creates ClusterRequest

The MCP operator automatically creates a `ClusterRequest` with the **same name**:

```yaml
apiVersion: clusters.openmcp.cloud/v1alpha1
kind: ClusterRequest
metadata:
  name: monitoring-cluster  # ← Same as MCP name
  namespace: mcp-{hash}     # Stable namespace based on MCP name
spec:
  purpose: MCP
```

### 3. User Creates Service Resource with Same Name

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: monitoring-cluster  # ← MUST match MCP name!
  namespace: default
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.10.0
  replicas: 1
```

### 4. Controller Finds ClusterRequest by Name

The `ClusterAccessReconciler` uses the service resource name to find the existing `ClusterRequest`:

```go
// In controller code
r.ClusterAccessReconciler.Reconcile(ctx, req)
// req.Name = "monitoring-cluster"
// Looks for ClusterRequest named "monitoring-cluster"
// Gets access to that MCP cluster
```

## Complete Example

### Step 1: Create MCP

```bash
kubectl apply -f - <<EOF
apiVersion: core.openmcp.cloud/v1alpha1
kind: ManagedControlPlaneV2
metadata:
  name: prod-ksm
  namespace: default
spec:
  provider: aws
  region: us-east-1
EOF
```

### Step 2: Wait for MCP to be Ready

```bash
kubectl get managedcontrolplanev2 prod-ksm -n default
# NAME       PHASE   AGE
# prod-ksm   Ready   1m
```

### Step 3: Verify ClusterRequest Exists

```bash
# Find the MCP namespace
MCP_NS=$(kubectl get managedcontrolplanev2 prod-ksm -n default -o jsonpath='{.status.namespace}')

# Check ClusterRequest
kubectl get clusterrequest prod-ksm -n $MCP_NS
# NAME       PURPOSE   PHASE     AGE
# prod-ksm   MCP       Granted   1m
```

### Step 4: Create KubeStateMetrics with Same Name

```bash
kubectl apply -f - <<EOF
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: prod-ksm  # ← MUST match MCP name
  namespace: default
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.10.0
  replicas: 1
  namespace: observability
EOF
```

### Step 5: Verify Deployment

```bash
# Check KubeStateMetrics status
kubectl get kubestatemetrics prod-ksm -n default
# NAME       PHASE   AGE
# prod-ksm   Ready   30s

# Check deployment on MCP
MCP_CONTEXT=$(kubectl get managedcontrolplanev2 prod-ksm -n default -o jsonpath='{.status.kubeconfigContext}')
kubectl get deploy,pods -n observability --context $MCP_CONTEXT
# NAME                                 READY   UP-TO-DATE   AVAILABLE   AGE
# deployment.apps/kube-state-metrics   1/1     1            1           30s
#
# NAME                                     READY   STATUS    RESTARTS   AGE
# pod/kube-state-metrics-xxx-yyy           1/1     Running   0          30s
```

## Multiple Resources on Same MCP

You can deploy multiple KubeStateMetrics resources to the **same MCP** by using the **same name**:

```yaml
# Both will deploy to the MCP named "monitoring-cluster"
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: monitoring-cluster  # ← Same name
  namespace: team-a
spec:
  namespace: team-a-observability
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.10.0
---
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: monitoring-cluster  # ← Same name
  namespace: team-b
spec:
  namespace: team-b-observability
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.10.0
```

Both will be deployed to the same MCP cluster because they reference the same `ClusterRequest` name.

## Error Scenarios

### Error 1: ClusterRequest Not Found

**Symptom:**
```
KubeStateMetrics stuck in "Progressing" phase
Controller logs: "failed to get MCP cluster: ClusterRequest 'wrong-name' not found"
```

**Cause:**
Service resource name doesn't match any ManagedControlPlaneV2 name.

**Solution:**
1. List available MCPs: `kubectl get managedcontrolplanev2 -A`
2. Update service resource name to match an MCP name
3. Or create a new MCP with the desired name

### Error 2: AccessRequest Not Granted

**Symptom:**
```
KubeStateMetrics stuck in "Progressing" phase
Controller logs: "AccessRequest not yet granted, requeueing..."
```

**Cause:**
ClusterRequest exists but AccessRequest is pending.

**Solution:**
Wait for the ClusterProvider to grant access. This is usually automatic.

### Error 3: MCP Not Ready

**Symptom:**
```
ManagedControlPlaneV2 shows Phase: "Provisioning"
Service resources can't be created yet
```

**Cause:**
MCP cluster is still being created.

**Solution:**
Wait for MCP to reach "Ready" phase before creating service resources.

## Design Rationale

### Why Naming Convention?

**Advantages:**
- ✅ Explicit control - user chooses which MCP by naming
- ✅ Simple implementation - no complex cluster selection logic
- ✅ Consistent with crossplane pattern
- ✅ Reusable - multiple resources can share same MCP

**Disadvantages:**
- ❌ Not obvious from API - requires documentation
- ❌ Name collisions - can't have different resources with same name on different MCPs
- ❌ No validation - controller doesn't check if MCP exists upfront

### Alternative Approaches Considered

1. **Add `spec.targetCluster` field**
   - Pro: Explicit in API
   - Con: Diverges from crossplane pattern
   - Status: Not implemented (could be future enhancement)

2. **Use labels/annotations**
   - Pro: More flexible
   - Con: More complex, harder to validate
   - Status: Not implemented

3. **Auto-create MCP**
   - Pro: Simpler user experience
   - Con: Less control, resource overhead
   - Status: Not implemented (use ClusterAccessManager for this pattern)

## Best Practices

### 1. Use Descriptive MCP Names

```yaml
# Good - describes purpose
name: production-monitoring
name: staging-metrics
name: team-alpha-observability

# Bad - too generic
name: cluster-1
name: test
name: mcp
```

### 2. Document MCP Naming in Your Organization

Create a naming convention guide for your teams:
```
{environment}-{purpose}
Examples:
- prod-monitoring
- staging-logging
- dev-metrics
```

### 3. Validate MCP Exists Before Creating Services

```bash
#!/bin/bash
MCP_NAME="monitoring-cluster"

# Check if MCP exists
if ! kubectl get managedcontrolplanev2 $MCP_NAME -n default &>/dev/null; then
  echo "Error: MCP '$MCP_NAME' not found. Create it first."
  exit 1
fi

# Check if MCP is ready
PHASE=$(kubectl get managedcontrolplanev2 $MCP_NAME -n default -o jsonpath='{.status.phase}')
if [ "$PHASE" != "Ready" ]; then
  echo "Error: MCP '$MCP_NAME' is not ready (phase: $PHASE)"
  exit 1
fi

# Now safe to create service resources
kubectl apply -f kubestatemetrics.yaml
```

### 4. Use GitOps for Consistency

Store MCP and service resources in the same directory:
```
kubernetes/
  monitoring-cluster/
    00-mcp.yaml           # ManagedControlPlaneV2
    10-config.yaml        # KubeStateMetricsConfig
    20-metrics.yaml       # KubeStateMetrics
```

This makes the naming relationship clear and ensures correct ordering.

## Troubleshooting

### Check ClusterRequest Status

```bash
# List all ClusterRequests
kubectl get clusterrequest -A

# Check specific ClusterRequest
MCP_NS=$(kubectl get managedcontrolplanev2 <name> -n <namespace> -o jsonpath='{.status.namespace}')
kubectl get clusterrequest <name> -n $MCP_NS -o yaml
```

### Check AccessRequest Status

```bash
# Find AccessRequest for your service
kubectl get accessrequest -A | grep <service-name>

# Check details
kubectl get accessrequest <name> -n <namespace> -o yaml
```

### Controller Logs

```bash
# Check service provider logs
kubectl logs deployment/sp-kubestatemetrics -n openmcp-system -f

# Filter for specific resource
kubectl logs deployment/sp-kubestatemetrics -n openmcp-system | grep <resource-name>
```

## Summary

- **Service resource name = MCP name** (this is the key rule)
- MCP operator creates ClusterRequest with same name
- Controller finds ClusterRequest by service resource name
- Resources deployed to that MCP cluster
- Document this convention for your users
- Validate MCP exists before creating service resources
