package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
	"github.com/openmcp-project/openmcp-testing/pkg/conditions"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)

// TestMCPNativeConfigManual sets up the full environment, deploys KubeStateMetrics
// without configRef, and waits for you to manually create the MCP-native ConfigMap.
//
// Run with:
//   go test -v ./test/e2e/ -run TestMCPNativeConfigManual -timeout 30m
//
// The test will:
//   1. Bootstrap platform, onboarding, MCP clusters
//   2. Deploy a KubeStateMetrics resource (no configRef)
//   3. Wait for it to become Ready
//   4. Print kubectl commands to apply the MCP ConfigMap
//   5. Wait for a signal file before tearing down
//
// To proceed with teardown, create the signal file:
//   touch /tmp/ksm-e2e-done
//
// To skip this test in normal runs, set: MANUAL_TEST=1
func TestMCPNativeConfigManual(t *testing.T) {
	if os.Getenv("MANUAL_TEST") != "1" {
		t.Skip("Skipping manual test; set MANUAL_TEST=1 to run")
	}

	var onboardingList unstructured.UnstructuredList

	manualTest := features.New("mcp native config manual test").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if _, err := resources.CreateObjectsFromDir(ctx, c, "platform"); err != nil {
				t.Fatalf("failed to create platform cluster objects: %v", err)
			}
			return ctx
		}).
		Setup(providers.CreateMCP("test-mcp")).
		Assess("deploy KubeStateMetrics and wait for manual ConfigMap",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				// Deploy KubeStateMetrics on onboarding cluster
				onboardingConfig, err := clusterutils.OnboardingConfig()
				if err != nil {
					t.Fatalf("failed to get onboarding config: %v", err)
				}
				objList, err := resources.CreateObjectsFromDir(ctx, onboardingConfig, "onboarding-mcp-native")
				if err != nil {
					t.Fatalf("failed to create onboarding cluster objects: %v", err)
				}
				for _, obj := range objList.Items {
					if err := wait.For(conditions.Match(&obj, onboardingConfig, "Ready", corev1.ConditionTrue)); err != nil {
						t.Error(err)
					}
				}
				objList.DeepCopyInto(&onboardingList)

				// Print instructions
				fmt.Println("\n" + strings.Repeat("=", 70))
				fmt.Println("  KubeStateMetrics is Ready on MCP cluster.")
				fmt.Println("  Now you can test the MCP-native ConfigMap feature.")
				fmt.Println(strings.Repeat("=", 70))

				// Find the MCP kind cluster context name
				fmt.Println("\nTo find the MCP cluster context:")
				fmt.Println("  kind get clusters | grep mcp")
				fmt.Println("")
				fmt.Println("Then apply the ConfigMap:")
				fmt.Println("  kubectl apply --context kind-<mcp-cluster-name> -f test/e2e/mcp/kube-state-metrics-config.yaml")
				fmt.Println("")
				fmt.Println("Check the deployment picks it up:")
				fmt.Println("  kubectl get deploy kube-state-metrics -n observability --context kind-<mcp-cluster-name> -o jsonpath='{.spec.template.metadata.annotations}'")
				fmt.Println("")
				fmt.Println("Check the KubeStateMetrics status on onboarding:")
				fmt.Println("  kubectl get kubestatemetrics test-mcp -o yaml --context kind-<onboarding-cluster-name>")
				fmt.Println("")
				fmt.Println("Update the ConfigMap data and re-reconcile to verify pod restart:")
				fmt.Println("  kubectl edit configmap kube-state-metrics-config -n observability --context kind-<mcp-cluster-name>")
				fmt.Println("")
				fmt.Println(strings.Repeat("-", 70))
				fmt.Println("  When done testing, create the signal file to tear down:")
				fmt.Println("  touch /tmp/ksm-e2e-done")
				fmt.Println(strings.Repeat("-", 70))

				// Remove stale signal file
				os.Remove("/tmp/ksm-e2e-done")

				// Poll for signal file
				for {
					if _, err := os.Stat("/tmp/ksm-e2e-done"); err == nil {
						fmt.Println("\nSignal received, proceeding with teardown...")
						os.Remove("/tmp/ksm-e2e-done")
						break
					}
					time.Sleep(5 * time.Second)
				}

				return ctx
			},
		).
		Teardown(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			onboardingConfig, err := clusterutils.OnboardingConfig()
			if err != nil {
				t.Error(err)
				return ctx
			}
			for _, obj := range onboardingList.Items {
				if err := resources.DeleteObject(ctx, onboardingConfig, &obj, wait.WithTimeout(time.Minute)); err != nil {
					t.Errorf("failed to delete onboarding object: %v", err)
				}
			}
			return ctx
		}).
		Teardown(providers.DeleteMCP("test-mcp", wait.WithTimeout(5*time.Minute)))
	testenv.Test(t, manualTest.Feature())
}
