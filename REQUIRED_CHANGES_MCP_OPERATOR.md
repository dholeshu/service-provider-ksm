# Required Changes for mcp-operator

This document describes the changes needed in `mcp-operator` to support KubeStateMetrics as a ManagedControlPlane component.

## Overview

To enable automatic KubeStateMetrics deployment via ManagedControlPlane spec, the following changes are required in the mcp-operator repository.

---

## 1. API Changes

### File: `api/core/v1alpha1/managedcontrolplane_types.go`

Add KubeStateMetrics to `ManagedControlPlaneComponents`:

```go
type ManagedControlPlaneComponents struct {
	// +kubebuilder:default={"type":"GardenerDedicated"}
	APIServer *APIServerConfiguration `json:"apiServer,omitempty"`

	Landscaper *LandscaperConfiguration `json:"landscaper,omitempty"`

	// KubeStateMetrics configuration for metrics export
	// +kubebuilder:validation:Optional
	KubeStateMetrics *KubeStateMetricsConfiguration `json:"kubeStateMetrics,omitempty"`

	CloudOrchestratorConfiguration `json:",inline"`
}
```

### File: `api/core/v1alpha1/kubestatemetrics_types.go` (NEW)

Create the KubeStateMetrics CRD:

```go
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const KubeStateMetricsComponent ComponentType = "KubeStateMetrics"

// KubeStateMetricsConfiguration represents the configuration for KubeStateMetrics in a ManagedControlPlane.
type KubeStateMetricsConfiguration struct {
	// Version of kube-state-metrics to deploy
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^v\d+\.\d+\.\d+$`
	// +kubebuilder:example="v2.18.0"
	Version string `json:"version"`

	// Number of replicas for kube-state-metrics deployment
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	Replicas *int32 `json:"replicas,omitempty"`

	// Namespace where kube-state-metrics will be deployed
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="observability"
	Namespace string `json:"namespace,omitempty"`

	// CustomResourceStateConfig contains the custom resource state configuration YAML
	// +kubebuilder:validation:Optional
	CustomResourceStateConfig string `json:"customResourceStateConfig,omitempty"`

	// ImagePullSecrets for pulling kube-state-metrics image
	// +kubebuilder:validation:Optional
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Resources for kube-state-metrics pod
	// +kubebuilder:validation:Optional
	Resources *ResourceRequirements `json:"resources,omitempty"`
}

// KubeStateMetricsSpec defines the desired state of KubeStateMetrics
type KubeStateMetricsSpec struct {
	KubeStateMetricsConfiguration `json:",inline"`
}

// KubeStateMetricsStatus defines the observed state of KubeStateMetrics
type KubeStateMetricsStatus struct {
	CommonStatusFields `json:",inline"`

	// DeploymentName is the name of the kube-state-metrics deployment
	DeploymentName string `json:"deploymentName,omitempty"`

	// ServiceName is the name of the kube-state-metrics service
	ServiceName string `json:"serviceName,omitempty"`

	// MetricsEndpoint is the URL for the metrics endpoint
	MetricsEndpoint string `json:"metricsEndpoint,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ksm
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// KubeStateMetrics is the Schema for the kube-state-metrics component
type KubeStateMetrics struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeStateMetricsSpec   `json:"spec,omitempty"`
	Status KubeStateMetricsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubeStateMetricsList contains a list of KubeStateMetrics
type KubeStateMetricsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeStateMetrics `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeStateMetrics{}, &KubeStateMetricsList{})
}

// Ensure KubeStateMetrics implements Component interface
var _ Component = &KubeStateMetrics{}

func (k *KubeStateMetrics) SetSpec(spec any) error {
	ksmSpec, ok := spec.(*KubeStateMetricsSpec)
	if !ok {
		return fmt.Errorf("expected *KubeStateMetricsSpec, got %T", spec)
	}
	k.Spec = *ksmSpec
	return nil
}

func (k *KubeStateMetrics) GetStatus() any {
	return k.Status
}

func (k *KubeStateMetrics) GetCommonStatus() *CommonStatusFields {
	return &k.Status.CommonStatusFields
}

// ExternalKubeStateMetricsStatus is the external status type for KubeStateMetrics
type ExternalKubeStateMetricsStatus struct {
	DeploymentName  string `json:"deploymentName,omitempty"`
	ServiceName     string `json:"serviceName,omitempty"`
	MetricsEndpoint string `json:"metricsEndpoint,omitempty"`
}

// ResourceRequirements defines CPU and memory resources
type ResourceRequirements struct {
	// Requests describes the minimum amount of compute resources required
	// +kubebuilder:validation:Optional
	Requests *ResourceList `json:"requests,omitempty"`

	// Limits describes the maximum amount of compute resources allowed
	// +kubebuilder:validation:Optional
	Limits *ResourceList `json:"limits,omitempty"`
}

// ResourceList is a set of (resource name, quantity) pairs
type ResourceList struct {
	// CPU resource
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="10m"
	CPU string `json:"cpu,omitempty"`

	// Memory resource
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="64Mi"
	Memory string `json:"memory,omitempty"`
}
```

---

## 2. Component Converter

### File: `internal/components/kubestatemetrics.go` (NEW)

```go
package components

import (
	"fmt"

	openmcpv1alpha1 "github.com/openmcp-project/mcp-operator/api/core/v1alpha1"
	openmcperrors "github.com/openmcp-project/mcp-operator/api/errors"
)

type KubeStateMetricsConverter struct{}

var _ Component = &openmcpv1alpha1.KubeStateMetrics{}
var _ ComponentConverter = &KubeStateMetricsConverter{}

// ConvertToResourceSpec implements ComponentConverter.
func (*KubeStateMetricsConverter) ConvertToResourceSpec(mcp *openmcpv1alpha1.ManagedControlPlane, _ *openmcpv1alpha1.InternalConfiguration) (any, error) {
	ksmConfig := mcp.Spec.Components.KubeStateMetrics
	if ksmConfig == nil {
		return nil, fmt.Errorf("kubestatemetrics configuration is missing")
	}

	res := &openmcpv1alpha1.KubeStateMetricsSpec{
		KubeStateMetricsConfiguration: *ksmConfig.DeepCopy(),
	}

	// Apply defaults
	if res.Namespace == "" {
		res.Namespace = "observability"
	}
	if res.Replicas == nil {
		defaultReplicas := int32(1)
		res.Replicas = &defaultReplicas
	}

	// Validate
	if res.Version == "" {
		return nil, fmt.Errorf("version is required for KubeStateMetrics")
	}

	return res, nil
}

// InjectStatus implements ComponentConverter.
func (*KubeStateMetricsConverter) InjectStatus(raw any, mcpStatus *openmcpv1alpha1.ManagedControlPlaneStatus) error {
	status, ok := raw.(openmcpv1alpha1.ExternalKubeStateMetricsStatus)
	if !ok {
		return openmcperrors.ErrWrongComponentStatusType
	}
	mcpStatus.Components.KubeStateMetrics = status.DeepCopy()
	return nil
}

// IsConfigured implements ComponentConverter.
func (*KubeStateMetricsConverter) IsConfigured(mcp *openmcpv1alpha1.ManagedControlPlane) bool {
	return mcp != nil && mcp.Spec.Components.KubeStateMetrics != nil
}
```

---

## 3. Component Registration

### File: `internal/components/registered_components.go`

Add KubeStateMetrics to the registry in the `init()` function:

```go
func init() {
	// ... existing registrations ...

	Registry.Register(openmcpv1alpha1.KubeStateMetricsComponent, func() *ComponentHandler {
		return NewComponentHandler(&openmcpv1alpha1.KubeStateMetrics{}, &KubeStateMetricsConverter{}, nil)
	})

	// add new components here
}
```

---

## 4. Component Controller

### File: `internal/controller/core/kubestatemetrics/controller.go` (NEW)

```go
package kubestatemetrics

import (
	"context"
	"fmt"

	"github.com/openmcp-project/mcp-operator/internal/utils"
	"github.com/openmcp-project/mcp-operator/internal/utils/components"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cconst "github.com/openmcp-project/mcp-operator/api/constants"
	openmcpv1alpha1 "github.com/openmcp-project/mcp-operator/api/core/v1alpha1"
	openmcperrors "github.com/openmcp-project/mcp-operator/api/errors"
)

const ControllerName = "KubeStateMetrics"

type KubeStateMetricsController struct {
	CrateClient client.Client // Onboarding cluster
	MCPClient   client.Client // MCP cluster
}

func NewKubeStateMetricsController(crateClient, mcpClient client.Client) *KubeStateMetricsController {
	return &KubeStateMetricsController{
		CrateClient: crateClient,
		MCPClient:   mcpClient,
	}
}

// +kubebuilder:rbac:groups=core.openmcp.cloud,resources=kubestatemetrics,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.openmcp.cloud,resources=kubestatemetrics/status,verbs=get;update;patch

func (r *KubeStateMetricsController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log, ctx := utils.InitializeControllerLogger(ctx, ControllerName)
	log.Debug(cconst.MsgStartReconcile)

	rr := r.reconcile(ctx, req)
	rr.LogRequeue(log, logging.DEBUG)
	if rr.Component == nil {
		return rr.Result, rr.ReconcileError
	}
	return components.UpdateStatus(ctx, r.CrateClient, rr)
}

func (r *KubeStateMetricsController) reconcile(ctx context.Context, req ctrl.Request) components.ReconcileResult[*openmcpv1alpha1.KubeStateMetrics] {
	log := logging.FromContextOrPanic(ctx)

	// Get KubeStateMetrics resource
	ksm := &openmcpv1alpha1.KubeStateMetrics{}
	if err := r.CrateClient.Get(ctx, req.NamespacedName, ksm); err != nil {
		if apierrors.IsNotFound(err) {
			log.Debug("Resource not found")
			return components.ReconcileResult[*openmcpv1alpha1.KubeStateMetrics]{}
		}
		return components.ReconcileResult[*openmcpv1alpha1.KubeStateMetrics]{
			ReconcileError: openmcperrors.WithReason(fmt.Errorf("unable to get resource: %w", err), cconst.ReasonCrateClusterInteractionProblem),
		}
	}

	// Handle operation annotation
	if ksm.GetAnnotations() != nil {
		op, ok := ksm.GetAnnotations()[openmcpv1alpha1.OperationAnnotation]
		if ok && op == openmcpv1alpha1.OperationAnnotationValueIgnore {
			log.Info("Ignoring resource due to ignore operation annotation")
			return components.ReconcileResult[*openmcpv1alpha1.KubeStateMetrics]{}
		}
	}

	// TODO: Implement deployment logic
	// 1. Create namespace on MCP cluster
	// 2. Create RBAC (ServiceAccount, ClusterRole, ClusterRoleBinding)
	// 3. Create ConfigMap (if custom config provided)
	// 4. Create Deployment
	// 5. Create Service
	// 6. Create PodDisruptionBudget (if replicas > 1)
	// 7. Update status with deployment info

	log.Info("KubeStateMetrics reconciliation not yet implemented - delegate to service-provider-ksm")

	return components.ReconcileResult[*openmcpv1alpha1.KubeStateMetrics]{
		Component: ksm,
		Conditions: []openmcpv1alpha1.ManagedControlPlaneComponentCondition{
			{
				Type:    "Ready",
				Status:  openmcpv1alpha1.ComponentConditionStatusTrue,
				Reason:  "DelegatedToServiceProvider",
				Message: "KubeStateMetrics deployment delegated to service-provider-ksm",
			},
		},
	}
}

func (r *KubeStateMetricsController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&openmcpv1alpha1.KubeStateMetrics{}).
		Complete(r)
}
```

---

## 5. ManagedControlPlane Status Update

### File: `api/core/v1alpha1/managedcontrolplane_types.go`

Add KubeStateMetrics status to `ManagedControlPlaneComponentsStatus`:

```go
type ManagedControlPlaneComponentsStatus struct {
	APIServer        *ExternalAPIServerStatus        `json:"apiServer,omitempty"`
	Landscaper       *ExternalLandscaperStatus       `json:"landscaper,omitempty"`
	KubeStateMetrics *ExternalKubeStateMetricsStatus `json:"kubeStateMetrics,omitempty"`
	CloudOrchestrator *ExternalCloudOrchestratorStatus `json:"cloudOrchestrator,omitempty"`
}
```

---

## 6. Register Controller in Main

### File: `cmd/manager/main.go`

Register the KubeStateMetrics controller:

```go
import (
	ksmctrl "github.com/openmcp-project/mcp-operator/internal/controller/core/kubestatemetrics"
)

func main() {
	// ... existing setup ...

	if err := (&ksmctrl.KubeStateMetricsController{
		CrateClient: mgr.GetClient(),
		MCPClient:   mcpClient, // Or appropriate cluster client
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KubeStateMetrics")
		os.Exit(1)
	}

	// ... rest of main ...
}
```

---

## 7. Generate CRDs

After making the API changes, regenerate the CRDs:

```bash
cd /path/to/mcp-operator
make generate
make manifests
```

This will generate:
- `config/crd/bases/core.openmcp.cloud_kubestatemetrics.yaml`
- `api/crds/manifests/core.openmcp.cloud_kubestatemetrics.yaml`

---

## Testing the Integration

After making these changes:

1. **Deploy updated mcp-operator**:
```bash
make docker-build docker-push
helm upgrade mcp-operator ./charts/mcp-operator
```

2. **Create ManagedControlPlane with KSM**:
```yaml
apiVersion: core.openmcp.cloud/v1alpha1
kind: ManagedControlPlane
metadata:
  name: test-mcp
  namespace: default
spec:
  components:
    apiServer:
      type: GardenerDedicated
    kubeStateMetrics:
      version: v2.18.0
      replicas: 1
      namespace: observability
```

3. **Verify KubeStateMetrics resource created**:
```bash
kubectl get kubestatemetrics -n default
# Should show: test-mcp
```

4. **Deploy service-provider-ksm** (if not using mcp-operator's controller):
The KubeStateMetrics resource will be picked up by service-provider-ksm if deployed.

---

## Alternative: Minimal Changes

If you want to **only create the resource** and let service-provider-ksm handle everything:

**Keep only changes 1-3** (API, Converter, Registration) and **skip the controller** (change 4).

The ManagedControlPlane controller will create the `KubeStateMetrics` resource, and service-provider-ksm will deploy it.

This is the recommended approach as it separates concerns:
- **mcp-operator**: Creates resources from ManagedControlPlane
- **service-provider-ksm**: Deploys and manages kube-state-metrics

---

## Summary of Changes

| File | Change | Required |
|------|--------|----------|
| `api/core/v1alpha1/managedcontrolplane_types.go` | Add KubeStateMetrics to Components | ✅ Yes |
| `api/core/v1alpha1/kubestatemetrics_types.go` | Create KubeStateMetrics CRD | ✅ Yes |
| `internal/components/kubestatemetrics.go` | Create converter | ✅ Yes |
| `internal/components/registered_components.go` | Register component | ✅ Yes |
| `internal/controller/core/kubestatemetrics/controller.go` | Create controller | ⚠️ Optional* |
| `cmd/manager/main.go` | Register controller | ⚠️ Optional* |

*Optional: Only needed if mcp-operator should deploy directly. Otherwise, service-provider-ksm handles deployment.

**Recommended**: Keep controller optional, let service-provider-ksm handle all deployment logic.
