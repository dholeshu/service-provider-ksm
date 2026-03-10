# Quick Reference: ClusterAccessReconciler Pattern

## TL;DR

**Service resource name = MCP name**

That's it. That's the whole pattern.

## Code Template

### Controller Struct
```go
type MyServiceReconciler struct {
    PlatformCluster         *clusters.Cluster
    OnboardingCluster       *clusters.Cluster
    ClusterAccessReconciler clusteraccess.Reconciler  // Add this
    Recorder                record.EventRecorder
    RecieveEventsChannel    <-chan event.GenericEvent
}
```

### Setup
```go
func (r *MyServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
    r.ClusterAccessReconciler = clusteraccess.NewClusterAccessReconciler(
        r.PlatformCluster.Client(),
        "myservice.services.openmcp.cloud",
    )
    r.ClusterAccessReconciler.
        WithMCPScheme(scheme.MCP).
        WithRetryInterval(10 * time.Second).
        WithMCPPermissions(getMyServiceMCPPermissions()).
        WithMCPRoleRefs(getMyServiceMCPRoleRefs()).
        SkipWorkloadCluster()

    return ctrl.NewControllerManagedBy(mgr).
        For(&v1alpha1.MyService{}).
        Complete(r)
}
```

### Reconcile
```go
func (r *MyServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // ... fetch resource ...

    // Get MCP cluster access
    mcpCluster, result, err := r.setupClusterAccess(ctx, req)
    if err != nil {
        return ctrl.Result{}, err
    }
    if result != nil {
        return *result, nil  // Requeue to wait for access
    }

    // Now use mcpCluster.Client() to deploy resources
    // ...
}
```

### setupClusterAccess Helper
```go
func (r *MyServiceReconciler) setupClusterAccess(ctx context.Context, req ctrl.Request) (*clusters.Cluster, *ctrl.Result, error) {
    log := log.FromContext(ctx)

    res, err := r.ClusterAccessReconciler.Reconcile(ctx, req)
    if err != nil {
        log.Error(err, "failed to reconcile cluster access")
        return nil, nil, err
    }

    if res.RequeueAfter > 0 {
        result := ctrl.Result{RequeueAfter: res.RequeueAfter}
        return nil, &result, nil
    }

    mcpCluster, err := r.ClusterAccessReconciler.MCPCluster(ctx, req)
    if err != nil {
        log.Error(err, "failed to get MCP cluster")
        result := ctrl.Result{RequeueAfter: 30 * time.Second}
        return nil, &result, nil
    }

    return mcpCluster, nil, nil
}
```

### Delete Handler
```go
func (r *MyServiceReconciler) handleDelete(ctx context.Context, req ctrl.Request, obj *v1alpha1.MyService) (ctrl.Result, error) {
    if !controllerutil.ContainsFinalizer(obj, MyFinalizer) {
        return ctrl.Result{}, nil
    }

    // Get cluster access for cleanup
    mcpCluster, result, err := r.setupClusterAccess(ctx, req)
    if err != nil {
        log.Error(err, "Failed to get MCP cluster for cleanup")
    } else if result == nil {
        if err := r.cleanup(ctx, obj, mcpCluster); err != nil {
            return ctrl.Result{}, err
        }
    }

    // Cleanup cluster access
    res, err := r.ClusterAccessReconciler.ReconcileDelete(ctx, req)
    if err != nil {
        return ctrl.Result{}, err
    }
    if res.RequeueAfter > 0 {
        return res, nil
    }

    // Remove finalizer
    controllerutil.RemoveFinalizer(obj, MyFinalizer)
    if err := r.OnboardingCluster.Client().Update(ctx, obj); err != nil {
        return ctrl.Result{}, err
    }

    return ctrl.Result{}, nil
}
```

### Permission Helpers
```go
func getMyServiceMCPPermissions() []clustersv1alpha1.PermissionsRequest {
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

func getMyServiceMCPRoleRefs() []commonapi.RoleRef {
    return []commonapi.RoleRef{
        {
            Kind: "ClusterRole",
            Name: "cluster-admin",
        },
    }
}
```

## User Workflow

### 1. Create MCP
```yaml
apiVersion: core.openmcp.cloud/v1alpha1
kind: ManagedControlPlaneV2
metadata:
  name: my-cluster
  namespace: default
```

### 2. Create Service Resource (Same Name!)
```yaml
apiVersion: myservice.services.openmcp.cloud/v1alpha1
kind: MyService
metadata:
  name: my-cluster  # ← MUST MATCH!
  namespace: default
```

## Common Mistakes

❌ **Different names**
```yaml
# MCP
name: prod-cluster

# Service
name: my-service  # ← Wrong! Names must match
```

✅ **Matching names**
```yaml
# MCP
name: prod-cluster

# Service
name: prod-cluster  # ← Correct!
```

❌ **Creating service before MCP**
```bash
kubectl apply -f service.yaml  # ← Wrong! MCP doesn't exist yet
kubectl apply -f mcp.yaml
```

✅ **Creating MCP first**
```bash
kubectl apply -f mcp.yaml
kubectl wait --for=condition=Ready managedcontrolplanev2/name
kubectl apply -f service.yaml  # ← Correct!
```

## Debugging

### Check if MCP exists
```bash
kubectl get managedcontrolplanev2 <name> -n <namespace>
```

### Check ClusterRequest
```bash
MCP_NS=$(kubectl get managedcontrolplanev2 <name> -n <namespace> -o jsonpath='{.status.namespace}')
kubectl get clusterrequest <name> -n $MCP_NS
```

### Check AccessRequest
```bash
kubectl get accessrequest -A | grep <service-name>
```

### Controller logs
```bash
kubectl logs deployment/sp-myservice -n openmcp-system -f | grep <resource-name>
```

## Key Points to Remember

1. **Naming is everything** - Service name must match MCP name
2. **MCP first** - Create MCP before service resources
3. **Wait for Ready** - Don't create services until MCP is ready
4. **Same name = same cluster** - Resources with same name go to same MCP
5. **Namespace doesn't matter** - It's the resource name that counts

## Import Requirements

```go
import (
    "github.com/openmcp-project/controller-utils/pkg/clusters"
    clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
    commonapi "github.com/openmcp-project/openmcp-operator/api/common"
    "github.com/openmcp-project/openmcp-operator/lib/clusteraccess"
)
```

## Scheme Setup

```go
// internal/scheme/scheme.go
var (
    Platform   = runtime.NewScheme()
    Onboarding = runtime.NewScheme()
    MCP        = runtime.NewScheme()  // Add this
)

func init() {
    // ... Platform and Onboarding ...

    // MCP cluster scheme
    utilruntime.Must(clientgoscheme.AddToScheme(MCP))
}
```

## That's It!

Follow this template, and you'll have proper MCP cluster targeting that works exactly like crossplane.
