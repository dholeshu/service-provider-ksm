# How Crossplane Solves MCP Cluster Targeting

**Investigation Date**: 2026-03-10
**Reference**: service-provider-crossplane at `/Users/i541517/SAPDevelop/openmcp-project/service-provider-crossplane`

## The Problem

In our service-provider-ksm, we faced an issue where each service resource created its own MCP cluster instead of using an existing one:

- `KubeStateMetricsConfig` → created `mcp-wifs54xa`
- `KubeStateMetrics` → created `mcp-gskbcurx`
- Existing `ManagedControlPlaneV2` "test" → not used

This happened because each resource called:
```go
mcpCluster, err := clusterAccessManager.CreateAndWaitForCluster(
    ctx,
    fmt.Sprintf("mcp-%s-%s", req.Namespace, req.Name),  // ← Unique name per resource
    clustersv1alpha1.PURPOSE_MCP,
    ...
)
```

## How Crossplane Solves This

### 1. Uses `ClusterAccessReconciler` Instead of `ClusterAccessManager`

**Key Difference:**
- `ClusterAccessManager` → **creates new ClusterRequests** (what we were using)
- `ClusterAccessReconciler` → **references existing ClusterRequests** (what crossplane uses)

### 2. References Existing ClusterRequest by Name

In `/Users/i541517/SAPDevelop/openmcp-project/openmcp-operator/lib/clusteraccess/clusteraccess.go`:

```go
r.internal.Register(
    advanced.ExistingClusterRequest(
        idMCP,
        suffixMCP,
        func(req reconcile.Request, _ ...any) (*commonapi.ObjectReference, error) {
            namespace, err := libutils.StableMCPNamespace(req.Name, req.Namespace)
            if err != nil {
                return nil, err
            }
            return &commonapi.ObjectReference{
                Name:      req.Name,      // ← Uses the service resource name!
                Namespace: namespace,
            }, nil
        }
    )
)
```

**What this does:**
- Looks for an **existing ClusterRequest** with name = `req.Name` (the service resource name)
- Does NOT create a new ClusterRequest
- Expects a ClusterRequest to already exist

### 3. ClusterRequest Created by ManagedControlPlaneV2

The workflow in OpenMCP:

```
1. User creates ManagedControlPlaneV2 resource:
   apiVersion: core.openmcp.cloud/v1alpha1
   kind: ManagedControlPlaneV2
   metadata:
     name: my-crossplane     ← This name matters!
     namespace: default

2. MCP Operator creates ClusterRequest:
   apiVersion: clusters.openmcp.cloud/v1alpha1
   kind: ClusterRequest
   metadata:
     name: my-crossplane     ← Same name as MCP!
     namespace: mcp-{hash}
   spec:
     purpose: MCP

3. Service resource references by name:
   apiVersion: crossplane.services.openmcp.cloud/v1alpha1
   kind: Crossplane
   metadata:
     name: my-crossplane     ← MUST match MCP name!
     namespace: default

4. ClusterAccessReconciler finds ClusterRequest:
   - Looks for ClusterRequest named "my-crossplane"
   - Gets access to that MCP cluster
   - All resources deployed there
```

### 4. Naming Convention is the Key

The critical insight: **The service resource name MUST match the ManagedControlPlaneV2 name**

This is the implicit API contract:
```yaml
# Step 1: Create MCP
apiVersion: core.openmcp.cloud/v1alpha1
kind: ManagedControlPlaneV2
metadata:
  name: prod-cluster
  namespace: default

# Step 2: Create service resource with SAME NAME
apiVersion: crossplane.services.openmcp.cloud/v1alpha1
kind: Crossplane
metadata:
  name: prod-cluster    # ← MUST match MCP name
  namespace: default
```

## Crossplane Controller Implementation

### Setup in SetupWithManager

From `/Users/i541517/SAPDevelop/openmcp-project/service-provider-crossplane/internal/controller/crossplane_controller.go:625`:

```go
func (r *CrossplaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
    r.ClusterAccessReconciler = clusteraccess.NewClusterAccessReconciler(
        r.PlatformCluster.Client(),
        controllerName,
    )
    r.ClusterAccessReconciler.
        WithMCPScheme(scheme.MCP).
        WithRetryInterval(10 * time.Second).
        WithMCPPermissions(getMCPPermissions()).
        WithMCPRoleRefs(getMCPRoleRefs()).
        SkipWorkloadCluster()  // Only MCP, no workload cluster

    return ctrl.NewControllerManagedBy(mgr).
        For(&v1alpha1.Crossplane{}).
        Complete(r)
}
```

### Usage in Reconcile

From `/Users/i541517/SAPDevelop/openmcp-project/service-provider-crossplane/internal/controller/crossplane_controller.go:172`:

```go
func (r *CrossplaneReconciler) setupClusterAccess(ctx context.Context, req ctrl.Request) (*clusters.Cluster, *ctrl.Result, error) {
    log := log.FromContext(ctx)

    // Create ClusterRequest/AccessRequest (or find existing)
    res, err := r.ClusterAccessReconciler.Reconcile(ctx, req)
    if err != nil {
        log.Error(err, "failed to reconcile cluster access for crossplane instance")
        return nil, nil, err
    }

    // AccessRequest was created but not yet granted
    if res.RequeueAfter > 0 {
        result := ctrl.Result{RequeueAfter: res.RequeueAfter}
        return nil, &result, nil
    }

    // Get MCPCluster for Crossplane instance
    mcpCluster, err := r.ClusterAccessReconciler.MCPCluster(ctx, req)
    if err != nil {
        log.Error(err, "failed to get MCP cluster for Crossplane instance")
        result := ctrl.Result{RequeueAfter: 30 * time.Second}
        return nil, &result, nil
    }

    return mcpCluster, nil, nil
}
```

**Key observation:**
- No cluster name generation
- No unique identifiers
- Just uses `req` (the reconcile request containing namespace/name)

## Why This Works

### The OpenMCP Platform Architecture

```
Platform Cluster (kind-platform)
  └─ ClusterProvider (openmcp-operator component)
      └─ Watches ManagedControlPlaneV2 resources
          └─ Creates ClusterRequest with same name

Onboarding Cluster (kind-onboarding)
  └─ User creates resources:
      ├─ ManagedControlPlaneV2: "prod-cluster"
      └─ Crossplane: "prod-cluster"  (same name)

Service Provider Controller
  └─ Uses ClusterAccessReconciler
      └─ Looks for ClusterRequest named "prod-cluster"
          └─ Finds the one created by MCP operator
              └─ Gets access to that specific MCP cluster
```

## Benefits of This Approach

1. **Explicit Control**: User controls which MCP to use by naming
2. **No Implicit Creation**: Doesn't automatically create MCPs
3. **Predictable**: Same name = same cluster, every time
4. **Reusable**: Multiple service resources can share same MCP (if named the same)
5. **Discoverable**: Easy to understand which resource uses which MCP

## Limitations

1. **Name Collision**: Service resource name must match MCP name
   - Can't have multiple resources on different MCPs with same name
   - Namespace isolation helps, but still a constraint

2. **Implicit Contract**: Not enforced by API types
   - No `spec.targetCluster` field
   - Relies on naming convention
   - Documentation critical

3. **No Validation**: Controller doesn't validate MCP exists before reconciling
   - Will fail later when ClusterRequest not found
   - Error message may be unclear

## How to Apply This to service-provider-ksm

### Option 1: Use Same Pattern (Naming Convention)

**Pros:**
- Consistent with crossplane pattern
- Simple implementation
- No API changes

**Cons:**
- Confusing that resource name determines MCP
- Not discoverable from API spec
- Name conflicts between resources

### Option 2: Add Explicit `spec.targetCluster` Field

**Pros:**
- Explicit and clear
- Better API design
- More flexible (multiple resources per MCP)

**Cons:**
- Diverges from crossplane pattern
- Requires API changes
- More complex implementation

### Option 3: Use Namespace Convention

**Pros:**
- Similar to name-based approach
- Better isolation

**Cons:**
- Still implicit
- Namespace management complexity

## Recommended Approach for KSM

**Use Option 1 initially** (naming convention) to stay consistent with crossplane:

```yaml
# Step 1: Create MCP
apiVersion: core.openmcp.cloud/v1alpha1
kind: ManagedControlPlaneV2
metadata:
  name: monitoring-mcp
  namespace: default

# Step 2: Create KSM with SAME NAME
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: monitoring-mcp    # ← Match MCP name
  namespace: default
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.10.0
  replicas: 1
```

**Document clearly:**
- The naming convention requirement
- Example manifests
- Error scenarios when MCP not found

**Consider Option 2 for future enhancement** if naming proves too limiting.

## Implementation Changes Needed

### 1. Update Controller Struct

```go
type KubeStateMetricsReconciler struct {
    PlatformCluster       *clusters.Cluster
    OnboardingCluster     *clusters.Cluster
    ClusterAccessReconciler clusteraccess.Reconciler  // ← New
    Recorder              record.EventRecorder
    RecieveEventsChannel  <-chan event.GenericEvent
}
```

### 2. Setup in SetupWithManager

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

func getMCPPermissions() []clustersv1alpha1.PermissionsRequest {
    return []clustersv1alpha1.PermissionsRequest{
        {
            Rules: []rbacv1.PolicyRule{
                {
                    APIGroups: []string{"*"},
                    Resources: []string{"*"},
                    Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
                },
            },
        },
    }
}

func getMCPRoleRefs() []commonapi.RoleRef {
    return []commonapi.RoleRef{
        {
            Kind: "ClusterRole",
            Name: "cluster-admin",
        },
    }
}
```

### 3. Update Reconcile Method

```go
func (r *KubeStateMetricsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)

    // Fetch the KubeStateMetrics instance
    ksm := &v1alpha1.KubeStateMetrics{}
    if err := r.OnboardingCluster.Client().Get(ctx, req.NamespacedName, ksm); err != nil {
        if apierrors.IsNotFound(err) {
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }

    // Setup cluster access (references existing ClusterRequest by name)
    mcpCluster, result, err := r.setupClusterAccess(ctx, req)
    if err != nil {
        return ctrl.Result{}, err
    }
    if result != nil {
        return *result, nil
    }

    // Deploy to MCP cluster
    deploymentReady, err := r.deployKubeStateMetrics(ctx, ksm, mcpCluster)
    // ... rest of reconcile
}

func (r *KubeStateMetricsReconciler) setupClusterAccess(ctx context.Context, req ctrl.Request) (*clusters.Cluster, *ctrl.Result, error) {
    log := log.FromContext(ctx)

    // Reconcile cluster access (finds existing ClusterRequest)
    res, err := r.ClusterAccessReconciler.Reconcile(ctx, req)
    if err != nil {
        log.Error(err, "failed to reconcile cluster access")
        return nil, nil, err
    }

    // Wait for AccessRequest to be granted
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

### 4. Update Delete Handler

```go
func (r *KubeStateMetricsReconciler) handleDelete(ctx context.Context, req ctrl.Request, ksm *v1alpha1.KubeStateMetrics) (ctrl.Result, error) {
    log := log.FromContext(ctx)

    if !controllerutil.ContainsFinalizer(ksm, KSMFinalizer) {
        return ctrl.Result{}, nil
    }

    // Get MCP cluster access
    mcpCluster, result, err := r.setupClusterAccess(ctx, req)
    if err != nil {
        log.Error(err, "failed to get MCP cluster for cleanup")
        // Continue with finalizer removal even if we can't clean up
    } else if result == nil {
        // Only cleanup if we got cluster access
        if err := r.cleanupKubeStateMetrics(ctx, ksm, mcpCluster); err != nil {
            log.Error(err, "failed to cleanup resources")
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
    if err := r.OnboardingCluster.Client().Update(ctx, ksm); err != nil {
        return ctrl.Result{}, err
    }

    return ctrl.Result{}, nil
}
```

## Testing the Solution

### 1. Create MCP First
```bash
kubectl apply -f - <<EOF
apiVersion: core.openmcp.cloud/v1alpha1
kind: ManagedControlPlaneV2
metadata:
  name: test-ksm
  namespace: default
spec:
  # MCP configuration
EOF
```

### 2. Wait for ClusterRequest
```bash
# Find the MCP namespace
MCP_NS=$(kubectl get managedcontrolplanev2 test-ksm -n default -o jsonpath='{.status.namespace}')

# Check ClusterRequest exists
kubectl get clusterrequest test-ksm -n $MCP_NS
```

### 3. Create KSM with Same Name
```bash
kubectl apply -f - <<EOF
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetrics
metadata:
  name: test-ksm    # ← MUST match MCP name
  namespace: default
spec:
  image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.10.0
  replicas: 1
EOF
```

### 4. Verify
```bash
# Check KSM status
kubectl get kubestatemetrics test-ksm -n default

# Check deployment on MCP
MCP_CONTEXT=$(kubectl get managedcontrolplanev2 test-ksm -n default -o jsonpath='{.status.kubeconfigContext}')
kubectl get deploy,pods -n observability --context $MCP_CONTEXT
```

## Conclusion

Crossplane solves the MCP targeting problem through:
1. **Naming convention**: Service resource name = MCP name
2. **ClusterAccessReconciler**: References existing ClusterRequests instead of creating new ones
3. **Platform architecture**: MCP operator creates ClusterRequests, service providers consume them

This is an **implicit contract** enforced by naming, not API validation.

For service-provider-ksm, we should adopt the same pattern for consistency with the OpenMCP ecosystem.
