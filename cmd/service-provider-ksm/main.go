/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"

	crdutil "github.com/openmcp-project/controller-utils/pkg/crds"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/dholeshu/service-provider-ksm/api/v1alpha1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/openmcp-operator/lib/clusteraccess"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"

	"github.com/spf13/cobra"

	"github.com/dholeshu/service-provider-ksm/api/crds"
	"github.com/dholeshu/service-provider-ksm/internal/controller"
	"github.com/dholeshu/service-provider-ksm/internal/scheme"
	// +kubebuilder:scaffold:imports
)

var (
	logger logging.Logger
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "service-provider-ksm",
		Short: "KubeStateMetrics service provider",
	}

	// run command
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the service-provider-ksm",
		RunE:  runCommand,
	}
	// add common flags to run command
	addCommonFlags(runCmd)
	// add specific flags to run command
	addMetricsFlags(runCmd)
	addWebhookFlags(runCmd)
	addManagerFlags(runCmd)

	// init command
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the service-provider-ksm",
		RunE:  initCommand,
	}
	// add common flags to init command
	addCommonFlags(initCmd)

	rootCmd.AddCommand(runCmd, initCmd)

	var err error
	logger, err = logging.GetLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get logger: %v\n", err)
		os.Exit(1)
	}

	ctrl.SetLogger(logger.Logr())
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func addCommonFlags(cmd *cobra.Command) {
	cmd.Flags().String("provider-name", "", "The name of the provider.")
	cmd.Flags().String("verbosity", "INFO", "The verbosity level of the provider.")
	cmd.Flags().String("environment", "", "The logical environment the provider is running in.")
}

func addMetricsFlags(cmd *cobra.Command) {
	cmd.Flags().String("metrics-bind-address", "0", "The address the metrics endpoint binds to. Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	cmd.Flags().Bool("metrics-secure", true, "If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	cmd.Flags().String("metrics-cert-path", "", "The directory that contains the metrics server certificate.")
	cmd.Flags().String("metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	cmd.Flags().String("metrics-cert-key", "tls.key", "The name of the metrics server key file.")
}

func addWebhookFlags(cmd *cobra.Command) {
	cmd.Flags().String("webhook-cert-path", "", "The directory that contains the webhook certificate.")
	cmd.Flags().String("webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	cmd.Flags().String("webhook-cert-key", "tls.key", "The name of the webhook key file.")
}

func addManagerFlags(cmd *cobra.Command) {
	cmd.Flags().String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().Bool("leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	cmd.Flags().Bool("enable-http2", false, "If set, HTTP/2 will be enabled for the metrics and webhook servers")
}

// initializePlatformCluster initializes the platform cluster with the necessary REST config and client.
func initializePlatformCluster() (*clusters.Cluster, error) {
	platformCluster := clusters.New("platform")

	platformCluster = platformCluster.WithRESTConfig(ctrl.GetConfigOrDie())

	if err := platformCluster.InitializeClient(scheme.Platform); err != nil {
		return nil, fmt.Errorf("failed to initialize client for platform cluster: %w", err)
	}
	return platformCluster, nil
}

func initCommand(cmd *cobra.Command, _ []string) error {
	providerName, err := cmd.Flags().GetString("provider-name")
	if err != nil {
		return fmt.Errorf("required flag --provider-name not set")
	}

	platformCluster, err := initializePlatformCluster()
	if err != nil {
		logger.Error(err, "Failed to initialize platform cluster")
		return err
	}

	runInit(platformCluster, providerName)
	return nil
}

// runInit installs the necessary CRDs on each cluster
func runInit(platformCluster *clusters.Cluster, providerName string) {
	ctx := context.Background()

	logger.Info("Running init command")

	clusterAccessManager := clusteraccess.NewClusterAccessManager(platformCluster.Client(), "ksm.services.openmcp.cloud", os.Getenv("POD_NAMESPACE"))
	clusterAccessManager.WithLogger(&logger).
		WithInterval(10 * time.Second).
		WithTimeout(30 * time.Minute)

	onboardingCluster, err := clusterAccessManager.CreateAndWaitForCluster(ctx, "onboarding-init", clustersv1alpha1.PURPOSE_ONBOARDING,
		scheme.Onboarding, []clustersv1alpha1.PermissionsRequest{
			{
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{"*"},
						Resources: []string{"*"},
						Verbs:     []string{"*"},
					},
				},
			},
		})

	if err != nil {
		logger.Error(err, "Failed to create and wait for onboarding cluster")
		os.Exit(1)
	}

	crdManager := crdutil.NewCRDManager(openmcpconst.ClusterLabel, crds.CRDs)

	crdManager.AddCRDLabelToClusterMapping(clustersv1alpha1.PURPOSE_PLATFORM, platformCluster)
	crdManager.AddCRDLabelToClusterMapping(clustersv1alpha1.PURPOSE_ONBOARDING, onboardingCluster)

	if err := crdManager.CreateOrUpdateCRDs(ctx, &logger); err != nil {
		logger.Error(err, "Failed to create or update CRDs")
		os.Exit(1)
	}

	ksmGVK := metav1.GroupVersionKind{
		Group:   v1alpha1.GroupVersion.Group,
		Version: v1alpha1.GroupVersion.Version,
		Kind:    "KubeStateMetrics",
	}
	ksmConfigGVK := metav1.GroupVersionKind{
		Group:   v1alpha1.GroupVersion.Group,
		Version: v1alpha1.GroupVersion.Version,
		Kind:    "KubeStateMetricsConfig",
	}
	logger.Info("Registering service-provider-ksm GVKs at the ServiceProvider object")
	if err := libutils.RegisterGVKsAtServiceProvider(ctx, platformCluster.Client(), providerName, ksmGVK, ksmConfigGVK); err != nil {
		logger.Error(err, "Failed to register service provider GVKs")
		os.Exit(1)
	}

	logger.Info("Init completed successfully")
}

// nolint:gocyclo
func runCommand(cmd *cobra.Command, _ []string) error {
	metricsAddr, _ := cmd.Flags().GetString("metrics-bind-address")
	metricsCertPath, _ := cmd.Flags().GetString("metrics-cert-path")
	metricsCertName, _ := cmd.Flags().GetString("metrics-cert-name")
	metricsCertKey, _ := cmd.Flags().GetString("metrics-cert-key")
	webhookCertPath, _ := cmd.Flags().GetString("webhook-cert-path")
	webhookCertName, _ := cmd.Flags().GetString("webhook-cert-name")
	webhookCertKey, _ := cmd.Flags().GetString("webhook-cert-key")
	enableLeaderElection, _ := cmd.Flags().GetBool("leader-elect")
	probeAddr, _ := cmd.Flags().GetString("health-probe-bind-address")
	secureMetrics, _ := cmd.Flags().GetBool("metrics-secure")
	enableHTTP2, _ := cmd.Flags().GetBool("enable-http2")

	var tlsOpts []func(*tls.Config)

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		logger.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts

	platformCluster, err := initializePlatformCluster()
	if err != nil {
		return err
	}

	ctx := context.Background()

	clusterAccessManager := clusteraccess.NewClusterAccessManager(platformCluster.Client(), "ksm.services.openmcp.cloud", os.Getenv("POD_NAMESPACE"))
	clusterAccessManager.WithLogger(&logger).
		WithInterval(10 * time.Second).
		WithTimeout(30 * time.Minute)

	onboardingCluster, err := clusterAccessManager.CreateAndWaitForCluster(ctx, "onboarding-run", clustersv1alpha1.PURPOSE_ONBOARDING,
		scheme.Onboarding, []clustersv1alpha1.PermissionsRequest{
			{
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{"*"},
						Resources: []string{"*"},
						Verbs:     []string{"*"},
					},
				},
			},
		})

	if err != nil {
		logger.Error(err, "Failed to create and wait for onboarding cluster")
		return err
	}

	if len(webhookCertPath) > 0 {
		logger.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(webhookCertPath, webhookCertName),
			filepath.Join(webhookCertPath, webhookCertKey),
		)
		if err != nil {
			return fmt.Errorf("failed to initialize webhook certificate watcher: %w", err)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: webhookTLSOpts,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	if len(metricsCertPath) > 0 {
		logger.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			return fmt.Errorf("failed to initialize metrics certificate watcher: %w", err)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	// Manager runs on onboarding cluster (not platform!)
	mgr, err := ctrl.NewManager(onboardingCluster.RESTConfig(), ctrl.Options{
		Scheme:                 scheme.Onboarding,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "232f9e39.openmcp.cloud",
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	// Add platform cluster to manager for ProviderConfig watching
	if err = mgr.Add(platformCluster.Cluster()); err != nil {
		return fmt.Errorf("unable to add platform cluster to manager: %w", err)
	}

	// Create a buffered channel for events between reconcilers
	reconcileEventsCh := make(chan event.GenericEvent, 128)

	ksmReconciler := &controller.KubeStateMetricsReconciler{
		PlatformCluster:      platformCluster,
		OnboardingCluster:    onboardingCluster,
		Recorder:             mgr.GetEventRecorderFor("sp-ksm-controller"),
		RecieveEventsChannel: reconcileEventsCh,
	}
	if err := ksmReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller KubeStateMetrics: %w", err)
	}

	ksmConfigReconciler := &controller.KubeStateMetricsConfigReconciler{
		PlatformCluster:      platformCluster,
		OnboardingCluster:    onboardingCluster,
		Recorder:             mgr.GetEventRecorderFor("sp-ksm-config-controller"),
		RecieveEventsChannel: reconcileEventsCh,
	}
	if err := ksmConfigReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller KubeStateMetricsConfig: %w", err)
	}

	providerConfigReconciler := &controller.ProviderConfigReconciler{
		PlatformCluster:   platformCluster,
		OnboardingCluster: onboardingCluster,
		SendEventsChannel: reconcileEventsCh,
	}
	if err := providerConfigReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller ProviderConfig: %w", err)
	}

	// +kubebuilder:scaffold:builder

	if metricsCertWatcher != nil {
		logger.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			return fmt.Errorf("unable to add metrics certificate watcher to manager: %w", err)
		}
	}

	if webhookCertWatcher != nil {
		logger.Info("Adding webhook certificate watcher to manager")
		if err := mgr.Add(webhookCertWatcher); err != nil {
			return fmt.Errorf("unable to add webhook certificate watcher to manager: %w", err)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	logger.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}
	return nil
}
