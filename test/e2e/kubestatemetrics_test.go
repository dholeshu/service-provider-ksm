package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
	"github.com/openmcp-project/openmcp-testing/pkg/conditions"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)

const (
	targetNamespace = "observability"
	configMapName   = "kube-state-metrics-config"

	crsFileArg = "--custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state-config.yaml"
	crsOnlyArg = "--custom-resource-state-only"
	stdCfgArg  = "--config=/etc/kube-state-metrics/config.yaml"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// deployKSMOnOnboarding creates the KubeStateMetrics resource on the onboarding
// cluster with a CRD retry loop (CRD may not be installed yet right after MCP
// creation).
func deployKSMOnOnboarding(list *unstructured.UnstructuredList) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		var objList *unstructured.UnstructuredList
		var lastErr error
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			onboardingConfig, err := clusterutils.OnboardingConfig()
			if err != nil {
				t.Error(err)
				return ctx
			}
			objList, lastErr = resources.CreateObjectsFromDir(ctx, onboardingConfig, "onboarding")
			if lastErr == nil {
				break
			}
			time.Sleep(5 * time.Second)
		}
		if lastErr != nil {
			t.Errorf("failed to create onboarding cluster objects: %v", lastErr)
			return ctx
		}
		objList.DeepCopyInto(list)
		return ctx
	}
}

// waitForKSMReady waits until every KubeStateMetrics object in list reports
// Ready=True (3 min timeout).
func waitForKSMReady(list *unstructured.UnstructuredList) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		onboardingConfig, err := clusterutils.OnboardingConfig()
		if err != nil {
			t.Error(err)
			return ctx
		}
		for _, obj := range list.Items {
			if obj.GetKind() == "KubeStateMetrics" {
				if err := wait.For(conditions.Match(&obj, onboardingConfig, "Ready", corev1.ConditionTrue),
					wait.WithTimeout(3*time.Minute)); err != nil {
					t.Error(err)
				}
			}
		}
		return ctx
	}
}

// createMCPConfigMap creates the kube-state-metrics-config ConfigMap on the MCP
// cluster with the given data keys. The caller must ensure the target namespace
// already exists (the controller creates it when the KubeStateMetrics resource
// becomes Ready).
func createMCPConfigMap(data map[string]string) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		mcpConfig, err := clusterutils.McpConfig()
		if err != nil {
			t.Error(err)
			return ctx
		}
		mcpClient, err := mcpConfig.NewClient()
		if err != nil {
			t.Error(err)
			return ctx
		}

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: targetNamespace,
			},
			Data: data,
		}
		if err := mcpClient.Resources(targetNamespace).Create(ctx, cm); err != nil {
			t.Errorf("failed to create ConfigMap: %v", err)
		}
		return ctx
	}
}

// verifyDeployment polls the MCP deployment until its args and volumes match
// the expectations. It uses a 3 min timeout to account for the controller's
// requeue interval (~1 min poll).
func verifyDeployment(wantCRSFileArg, wantCRSOnlyArg, wantStdCfgArg, wantVolumeMount bool) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		mcpConfig, err := clusterutils.McpConfig()
		if err != nil {
			t.Error(err)
			return ctx
		}
		mcpClient, err := mcpConfig.NewClient()
		if err != nil {
			t.Error(err)
			return ctx
		}

		var lastDeployment appsv1.Deployment
		pollErr := wait.For(func(ctx context.Context) (bool, error) {
			var dep appsv1.Deployment
			if err := mcpClient.Resources(targetNamespace).Get(ctx, "kube-state-metrics", targetNamespace, &dep); err != nil {
				return false, nil // not found yet, keep polling
			}
			lastDeployment = dep
			return checkDeploymentState(&dep, wantCRSFileArg, wantCRSOnlyArg, wantStdCfgArg, wantVolumeMount), nil
		}, wait.WithTimeout(3*time.Minute))

		if pollErr != nil {
			// Log the actual deployment state for debugging.
			if len(lastDeployment.Spec.Template.Spec.Containers) > 0 {
				t.Errorf("deployment args mismatch (timeout): args=%v, volumes=%v",
					lastDeployment.Spec.Template.Spec.Containers[0].Args,
					volumeNames(lastDeployment.Spec.Template.Spec.Volumes))
			} else {
				t.Errorf("deployment not found within timeout: %v", pollErr)
			}
		}
		return ctx
	}
}

// teardownOnboardingObjects deletes every object that was created from the
// onboarding directory.
func teardownOnboardingObjects(list *unstructured.UnstructuredList) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		onboardingConfig, err := clusterutils.OnboardingConfig()
		if err != nil {
			t.Error(err)
			return ctx
		}
		for _, obj := range list.Items {
			if err := resources.DeleteObject(ctx, onboardingConfig, &obj, wait.WithTimeout(time.Minute)); err != nil {
				t.Errorf("failed to delete onboarding object: %v", err)
			}
		}
		return ctx
	}
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

// checkDeploymentState returns true when the deployment's first container args
// and pod volumes match the caller's expectations.
func checkDeploymentState(dep *appsv1.Deployment, wantCRSFileArg, wantCRSOnlyArg, wantStdCfgArg, wantVolumeMount bool) bool {
	containers := dep.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		return false
	}
	args := containers[0].Args

	if containsArg(args, crsFileArg) != wantCRSFileArg {
		return false
	}
	if containsArg(args, crsOnlyArg) != wantCRSOnlyArg {
		return false
	}
	if containsArg(args, stdCfgArg) != wantStdCfgArg {
		return false
	}

	hasVol := hasVolume(dep.Spec.Template.Spec.Volumes, "config")
	hasMount := hasVolumeMount(containers[0].VolumeMounts, "config")
	if hasVol != wantVolumeMount || hasMount != wantVolumeMount {
		return false
	}
	return true
}

func containsArg(args []string, target string) bool {
	for _, a := range args {
		if strings.Contains(a, target) {
			return true
		}
	}
	return false
}

func hasVolume(volumes []corev1.Volume, name string) bool {
	for _, v := range volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}

func hasVolumeMount(mounts []corev1.VolumeMount, name string) bool {
	for _, m := range mounts {
		if m.Name == name {
			return true
		}
	}
	return false
}

func volumeNames(volumes []corev1.Volume) []string {
	names := make([]string, len(volumes))
	for i, v := range volumes {
		names[i] = v.Name
	}
	return names
}

// ---------------------------------------------------------------------------
// Test: no ConfigMap → no extra args, no volume
// ---------------------------------------------------------------------------

func TestNoConfigMap(t *testing.T) {
	var onboardingList unstructured.UnstructuredList

	feat := features.New("no configmap").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if _, err := resources.CreateObjectsFromDir(ctx, c, "platform"); err != nil {
				t.Errorf("failed to create platform objects: %v", err)
			}
			return ctx
		}).
		Setup(providers.CreateMCP("test-mcp")).
		Assess("deploy KubeStateMetrics", deployKSMOnOnboarding(&onboardingList)).
		Assess("wait for Ready", waitForKSMReady(&onboardingList)).
		Assess("verify deployment has no config args or volume",
			verifyDeployment(false, false, false, false)).
		Teardown(teardownOnboardingObjects(&onboardingList)).
		Teardown(providers.DeleteMCP("test-mcp", wait.WithTimeout(5*time.Minute)))

	testenv.Test(t, feat.Feature())
}

// ---------------------------------------------------------------------------
// Test: CRS config only → --custom-resource-state-config-file + --custom-resource-state-only
// ---------------------------------------------------------------------------

func TestCRSConfigOnly(t *testing.T) {
	var onboardingList unstructured.UnstructuredList

	feat := features.New("crs config only").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if _, err := resources.CreateObjectsFromDir(ctx, c, "platform"); err != nil {
				t.Errorf("failed to create platform objects: %v", err)
			}
			return ctx
		}).
		Setup(providers.CreateMCP("test-mcp")).
		Assess("deploy KubeStateMetrics", deployKSMOnOnboarding(&onboardingList)).
		Assess("wait for Ready", waitForKSMReady(&onboardingList)).
		Assess("create CRS-only ConfigMap", createMCPConfigMap(map[string]string{
			"custom-resource-state-config.yaml": "kind: CustomResourceStateMetrics\nspec:\n  resources: []\n",
		})).
		Assess("verify CRS args and volume",
			verifyDeployment(true, true, false, true)).
		Teardown(teardownOnboardingObjects(&onboardingList)).
		Teardown(providers.DeleteMCP("test-mcp", wait.WithTimeout(5*time.Minute)))

	testenv.Test(t, feat.Feature())
}

// ---------------------------------------------------------------------------
// Test: standard config only → --config, no CRS args
// ---------------------------------------------------------------------------

func TestStdConfigOnly(t *testing.T) {
	var onboardingList unstructured.UnstructuredList

	feat := features.New("std config only").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if _, err := resources.CreateObjectsFromDir(ctx, c, "platform"); err != nil {
				t.Errorf("failed to create platform objects: %v", err)
			}
			return ctx
		}).
		Setup(providers.CreateMCP("test-mcp")).
		Assess("deploy KubeStateMetrics", deployKSMOnOnboarding(&onboardingList)).
		Assess("wait for Ready", waitForKSMReady(&onboardingList)).
		Assess("create std-only ConfigMap", createMCPConfigMap(map[string]string{
			"config.yaml": "resources:\n  - pods\n",
		})).
		Assess("verify std config arg and volume",
			verifyDeployment(false, false, true, true)).
		Teardown(teardownOnboardingObjects(&onboardingList)).
		Teardown(providers.DeleteMCP("test-mcp", wait.WithTimeout(5*time.Minute)))

	testenv.Test(t, feat.Feature())
}

// ---------------------------------------------------------------------------
// Test: both configs → --custom-resource-state-config-file + --config, no --custom-resource-state-only
// ---------------------------------------------------------------------------

func TestBothConfigs(t *testing.T) {
	var onboardingList unstructured.UnstructuredList

	feat := features.New("both configs").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if _, err := resources.CreateObjectsFromDir(ctx, c, "platform"); err != nil {
				t.Errorf("failed to create platform objects: %v", err)
			}
			return ctx
		}).
		Setup(providers.CreateMCP("test-mcp")).
		Assess("deploy KubeStateMetrics", deployKSMOnOnboarding(&onboardingList)).
		Assess("wait for Ready", waitForKSMReady(&onboardingList)).
		Assess("create ConfigMap with both keys", createMCPConfigMap(map[string]string{
			"custom-resource-state-config.yaml": "kind: CustomResourceStateMetrics\nspec:\n  resources: []\n",
			"config.yaml":                       "resources:\n  - pods\n",
		})).
		Assess("verify both config args, no crs-only, volume mounted",
			verifyDeployment(true, false, true, true)).
		Teardown(teardownOnboardingObjects(&onboardingList)).
		Teardown(providers.DeleteMCP("test-mcp", wait.WithTimeout(5*time.Minute)))

	testenv.Test(t, feat.Feature())
}
