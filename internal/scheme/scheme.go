package scheme

import (
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	mcpv1alpha1 "github.com/openmcp-project/mcp-operator/api/core/v1alpha1"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	providersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"

	kubestatemetricssv1alpha1 "github.com/dholeshu/service-provider-ksm/api/v1alpha1"
)

var (
	// Platform scheme includes types needed on the platform cluster
	Platform = runtime.NewScheme()
	// Onboarding scheme includes types needed on the onboarding cluster
	Onboarding = runtime.NewScheme()
	// MCP scheme includes types needed on the MCP cluster
	MCP = runtime.NewScheme()
)

func init() {
	// Platform cluster scheme (ProviderConfig, Clusters, AccessRequests, ServiceProvider)
	utilruntime.Must(clientgoscheme.AddToScheme(Platform))
	utilruntime.Must(apiextensionv1.AddToScheme(Platform))
	utilruntime.Must(kubestatemetricssv1alpha1.AddToScheme(Platform))
	utilruntime.Must(clustersv1alpha1.AddToScheme(Platform))
	utilruntime.Must(providersv1alpha1.AddToScheme(Platform))

	// Onboarding cluster scheme (KubeStateMetrics, KubeStateMetricsConfig, ManagedControlPlane)
	utilruntime.Must(clientgoscheme.AddToScheme(Onboarding))
	utilruntime.Must(apiextensionv1.AddToScheme(Onboarding))
	utilruntime.Must(kubestatemetricssv1alpha1.AddToScheme(Onboarding))
	utilruntime.Must(mcpv1alpha1.AddToScheme(Onboarding))
	utilruntime.Must(clustersv1alpha1.AddToScheme(Onboarding))

	// MCP cluster scheme (Kubernetes core types + ConfigMaps + Deployments)
	utilruntime.Must(clientgoscheme.AddToScheme(MCP))
}
