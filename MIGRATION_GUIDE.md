# Migration Guide: ClusterAccessManager to ClusterAccessReconciler

## Overview

This guide explains the changes made to service-provider-ksm to fix the multi-MCP cluster issue and align with the crossplane pattern.

## What Changed

### Old Behavior (Before)

- Each service resource created its own MCP cluster
- Used `ClusterAccessManager.CreateAndWaitForCluster()`
- Generated unique cluster names: `mcp-{namespace}-{name}`
- Result: Multiple MCP clusters created unintentionally

### New Behavior (After)

- Service resources reference existing MCP clusters by name
- Uses `ClusterAccessReconciler` pattern
- Service resource name must match ManagedControlPlaneV2 name
- Result: All resources with same name deploy to same MCP

## Code Changes Summary

### 1. Controller Struct

**Before:**
```go
type KubeStateMetricsReconciler struct {
    PlatformCluster      *clusters.Cluster
    OnboardingCluster    *clusters.Cluster
    Recorder             record.EventRecorder
    RecieveEventsChannel <-chan event.GenericEvent
}
```

**After:**
```go
type KubeStateMetricsReconciler struct {
    PlatformCluster         *clusters.Cluster
    OnboardingCluster       *clusters.Cluster
    ClusterAccessReconciler clusteraccess.Reconciler  // ← Added
    Recorder                record.EventRecorder
    RecieveEventsChannel    <-chan event.GenericEvent
}
```

### 2. SetupWithManager

**Before:**
```go
func (r *KubeStateMetricsReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&v1alpha1.KubeStateMetrics{}).
        Complete(r)
}
```

**After:**
```go
func (r *KubeStateMetricsReconciler) SetupWithManager(mgr ctrl.Manager) error {
    r.ClusterAccessReconciler = clusteraccess.NewClusterAccessReconciler(
        r.PlatformCluster.Client(),
        "ksm.services.openmcp.cloud",
    )
    r.ClusterAccessReconciler.
        WithMCPScheme(scheme.MCP).
        WithRetryInterval(10 * time.Second).
        WithMCPPermissions(getMCPPermissions()).
        WithMCPRoleRefs(getMCPRoleRefs()).
        SkipWorkloadCluster()

    return ctrl.NewControllerManagedBy(mgr).
        For(&v1alpha1.KubeStateMetrics{}).
        Complete(r)
}
```

### 3. Reconcile Method

**Before:**
```go
// Get MCP cluster access for this resource
clusterAccessManager := clusteraccess.NewClusterAccessManager(
    r.PlatformCluster.Client(),
    "ksm.services.openmcp.cloud",
    os.Getenv("POD_NAMESPACE"),
)

mcpCluster, err := clusterAccessManager.CreateAndWaitForCluster(
    ctx,
    fmt.Sprintf("mcp-%s-%s", req.Namespace, req.Name),  // ← Unique name
    clustersv1alpha1.PURPOSE_MCP,
    scheme.Onboarding,
    []clustersv1alpha1.PermissionsRequest{...},
)
```

**After:**
```go
// Setup cluster access (references existing ClusterRequest by name)
mcpCluster, result, err := r.setupClusterAccess(ctx, req)
if err != nil {
    log.Error(err, "Failed to setup cluster access")
    ksm.Status.Phase = "Error"
    r.OnboardingCluster.Client().Status().Patch(ctx, ksm, client.MergeFrom(oldKsm))
    return ctrl.Result{}, err
}
if result != nil {
    // Requeue to wait for cluster access
    return *result, nil
}
```

### 4. New setupClusterAccess Method

**Added:**
```go
func (r *KubeStateMetricsReconciler) setupClusterAccess(ctx context.Context, req ctrl.Request) (*clusters.Cluster, *ctrl.Result, error) {
    log := log.FromContext(ctx)

    // Reconcile cluster access (finds existing ClusterRequest by name)
    res, err := r.ClusterAccessReconciler.Reconcile(ctx, req)
    if err != nil {
        log.Error(err, "failed to reconcile cluster access")
        return nil, nil, err
    }

    // AccessRequest was created but not yet granted
    if res.RequeueAfter > 0 {
        result := ctrl.Result{RequeueAfter: res.RequeueAfter}
        return nil, &result, nil
    }

    // Get MCP cluster client
    mcpCluster, err := r.ClusterAccessReconciler.MCPCluster(ctx, req)
    if err != nil {
        log.Error(err, "failed to get MCP cluster")
        result := ctrl.Result{RequeueAfter: 30 * time.Second}
        return nil, &result, nil
    }

    return mcpCluster, nil, nil
}
```

### 5. Updated Delete Handler

**Before:**
```go
func (r *KubeStateMetricsReconciler) handleDelete(ctx context.Context, req ctrl.Request, ksm *v1alpha1.KubeStateMetrics) (ctrl.Result, error) {
    // ... get cluster access same as Reconcile ...

    // Clean up resources
    if err := r.cleanupKubeStateMetrics(ctx, ksm, mcpCluster); err != nil {
        return ctrl.Result{}, err
    }

    // Remove finalizer
    controllerutil.RemoveFinalizer(ksm, KSMFinalizer)
    return ctrl.Result{}, nil
}
```

**After:**
```go
func (r *KubeStateMetricsReconciler) handleDelete(ctx context.Context, req ctrl.Request, ksm *v1alpha1.KubeStateMetrics) (ctrl.Result, error) {
    // Get MCP cluster access
    mcpCluster, result, err := r.setupClusterAccess(ctx, req)
    if err != nil {
        log.Error(err, "Failed to get MCP cluster access for cleanup")
    } else if result == nil {
        // Only cleanup if we got cluster access
        if err := r.cleanupKubeStateMetrics(ctx, ksm, mcpCluster); err != nil {
            return ctrl.Result{}, err
        }
    }

    // Reconcile delete for cluster access
    res, err := r.ClusterAccessReconciler.ReconcileDelete(ctx, req)
    if err != nil {
        return ctrl.Result{}, err
    }
    if res.RequeueAfter > 0 {
        return res, nil
    }

    // Remove finalizer
    controllerutil.RemoveFinalizer(ksm, KSMFinalizer)
    return ctrl.Result{}, nil
}
```

### 6. Scheme Updates

**Added MCP scheme:**
```go
// internal/scheme/scheme.go
var (
    Platform   = runtime.NewScheme()
    Onboarding = runtime.NewScheme()
    MCP        = runtime.NewScheme()  // ← Added
)

func init() {
    // ... Platform and Onboarding initialization ...

    // MCP cluster scheme
    utilruntime.Must(clientgoscheme.AddToScheme(MCP))
}
```

## Migration Steps for Users

### If You Have Existing Deployments

**Important:** Existing deployments created with the old pattern will continue to work but use separate MCP clusters.

#### Option 1: Keep Existing Setup (No Changes)

If you're okay with having separate MCP clusters for each resource, you don't need to do anything. The old deployments will continue to work.

#### Option 2: Migrate to Single MCP (Recommended)

1. **Identify existing MCP clusters:**
   ```bash
   kubectl get managedcontrolplanev2 -A
   kubectl get clusterrequest -A
   ```

2. **Choose one MCP to keep** (or create a new one with desired name)

3. **Create resources with matching names:**
   ```yaml
   # If you have MCP named "prod-monitoring"
   apiVersion: ksm.services.openmcp.cloud/v1alpha1
   kind: KubeStateMetrics
   metadata:
     name: prod-monitoring  # ← Match MCP name
     namespace: default
   spec:
     # ... your config
   ```

4. **Delete old resources:**
   ```bash
   kubectl delete kubestatemetrics old-name -n old-namespace
   ```

5. **Clean up unused MCPs** (optional):
   ```bash
   # Be careful - only delete MCPs you created unintentionally
   kubectl delete managedcontrolplanev2 <unused-mcp-name> -n <namespace>
   ```

### For New Deployments

1. **Create ManagedControlPlaneV2 first:**
   ```bash
   kubectl apply -f mcp.yaml
   ```

2. **Wait for it to be ready:**
   ```bash
   kubectl get managedcontrolplanev2 <name> -n <namespace>
   ```

3. **Create service resources with matching name:**
   ```bash
   kubectl apply -f kubestatemetrics.yaml  # name matches MCP
   ```

## Breaking Changes

### ⚠️ Naming Convention Now Required

**Old:** Service resources could have any name, and MCP clusters were created automatically.

**New:** Service resource name must match an existing ManagedControlPlaneV2 name.

**Impact:**
- Existing manifests may need name changes
- Must create MCP before service resources
- Name collisions possible if resources in different namespaces need different MCPs

### ⚠️ Manual MCP Creation Required

**Old:** MCPs were created automatically by ClusterAccessManager.

**New:** MCPs must be created explicitly by users or MCP operator.

**Impact:**
- Users must understand MCP concept
- Requires additional step in deployment workflow
- More control but more complexity

### ⚠️ ClusterRequest Must Exist

**Old:** ClusterRequest was created automatically.

**New:** ClusterRequest must exist (created by MCP operator).

**Impact:**
- Errors if ClusterRequest not found
- Must wait for MCP to be ready before creating services
- Better error messages but more failure modes

## Troubleshooting Migration Issues

### Error: "ClusterRequest not found"

**Cause:** Service resource name doesn't match any MCP name.

**Solution:**
```bash
# List available MCPs
kubectl get managedcontrolplanev2 -A

# Update your service resource name to match
kubectl edit kubestatemetrics <name> -n <namespace>
```

### Error: "AccessRequest not granted"

**Cause:** ClusterRequest exists but AccessRequest is pending.

**Solution:**
Wait for ClusterProvider to grant access. Check ClusterProvider logs if it takes too long.

### Multiple MCP Clusters Created

**Cause:** Created service resources with different names before migration.

**Solution:**
1. Identify all MCP clusters: `kubectl get managedcontrolplanev2 -A`
2. Decide which to keep
3. Recreate service resources with matching name
4. Delete unused MCPs

### Resources Not Deploying to MCP

**Cause:** Name mismatch between service resource and MCP.

**Solution:**
Verify names match exactly:
```bash
# Check MCP name
kubectl get managedcontrolplanev2 -A

# Check service resource name
kubectl get kubestatemetrics -A

# Names must match exactly (case-sensitive)
```

## Testing the Migration

### 1. Create Test MCP

```bash
kubectl apply -f - <<EOF
apiVersion: core.openmcp.cloud/v1alpha1
kind: ManagedControlPlaneV2
metadata:
  name: test-migration
  namespace: default
spec:
  provider: kind
  region: local
EOF
```

### 2. Wait for Ready

```bash
kubectl wait --for=condition=Ready managedcontrolplanev2/test-migration -n default --timeout=5m
```

### 3. Create Test KubeStateMetrics

```bash
kubectl apply -f - <<EOF
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: test-migration  # ← Matches MCP name
  namespace: default
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.10.0
  replicas: 1
EOF
```

### 4. Verify Deployment

```bash
# Check status
kubectl get kubestatemetrics test-migration -n default

# Should show: Phase: Ready

# Check on MCP
MCP_CONTEXT=$(kubectl get managedcontrolplanev2 test-migration -n default -o jsonpath='{.status.kubeconfigContext}')
kubectl get deploy -n observability --context $MCP_CONTEXT
```

### 5. Clean Up

```bash
kubectl delete kubestatemetrics test-migration -n default
kubectl delete managedcontrolplanev2 test-migration -n default
```

## Rollback Plan

If you need to rollback to the old behavior:

1. **Revert code changes:**
   ```bash
   git revert <commit-hash>
   ```

2. **Rebuild and redeploy:**
   ```bash
   make docker-build
   kubectl rollout restart deployment/sp-kubestatemetrics -n openmcp-system
   ```

3. **Update manifests:**
   - Remove name matching requirement
   - Service resources can have any name again

**Note:** This will leave orphaned MCP clusters that need manual cleanup.

## Support

For issues during migration:

1. Check controller logs: `kubectl logs deployment/sp-kubestatemetrics -n openmcp-system`
2. Verify MCP status: `kubectl get managedcontrolplanev2 -A`
3. Check ClusterRequests: `kubectl get clusterrequest -A`
4. Review [MCP_TARGETING.md](./MCP_TARGETING.md) for detailed guidance
5. Open an issue with:
   - Current setup description
   - Error messages from logs
   - Output of `kubectl get managedcontrolplanev2,kubestatemetrics -A`
