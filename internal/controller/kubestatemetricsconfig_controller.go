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

package controller

import (
	"context"
	"fmt"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	"github.com/openmcp-project/openmcp-operator/lib/clusteraccess"

	v1alpha1 "github.com/dholeshu/service-provider-ksm/api/v1alpha1"
	"github.com/dholeshu/service-provider-ksm/internal/scheme"
)

const (
	configMapSuffix = "-ksm-config"
	defaultTargetNs = "observability"
)

var (
	ConfigMapFinalizer = v1alpha1.GroupVersion.Group + "/config-finalizer"
)

// KubeStateMetricsConfigReconciler reconciles a KubeStateMetricsConfig object
type KubeStateMetricsConfigReconciler struct {
	PlatformCluster         *clusters.Cluster
	OnboardingCluster       *clusters.Cluster
	ClusterAccessReconciler clusteraccess.Reconciler
	Recorder                record.EventRecorder
	RecieveEventsChannel    <-chan event.GenericEvent
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeStateMetricsConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.ClusterAccessReconciler = clusteraccess.NewClusterAccessReconciler(
		r.PlatformCluster.Client(),
		"ksm.services.openmcp.cloud",
	)
	r.ClusterAccessReconciler.
		WithMCPScheme(scheme.MCP).
		WithRetryInterval(10 * time.Second).
		WithMCPPermissions(getConfigMCPPermissions()).
		WithMCPRoleRefs(getConfigMCPRoleRefs()).
		SkipWorkloadCluster()

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.KubeStateMetricsConfig{}).
		Complete(r)
}

// Reconcile reconciles the KubeStateMetricsConfig instance.
func (r *KubeStateMetricsConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the KubeStateMetricsConfig instance from the onboarding cluster
	config := &v1alpha1.KubeStateMetricsConfig{}
	if err := r.OnboardingCluster.Client().Get(ctx, req.NamespacedName, config); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get KubeStateMetricsConfig")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !config.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, req, config)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(config, ConfigMapFinalizer) {
		controllerutil.AddFinalizer(config, ConfigMapFinalizer)
		if err := r.OnboardingCluster.Client().Update(ctx, config); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Update status to Progressing
	oldConfig := config.DeepCopy()
	config.Status.Phase = "Progressing"
	config.Status.ObservedGeneration = config.Generation

	// Setup cluster access (references existing ClusterRequest by name)
	mcpCluster, result, err := r.setupClusterAccess(ctx, req)
	if err != nil {
		log.Error(err, "Failed to setup cluster access")
		config.Status.Phase = "Error"
		r.OnboardingCluster.Client().Status().Patch(ctx, config, client.MergeFrom(oldConfig))
		return ctrl.Result{}, err
	}
	if result != nil {
		// Requeue to wait for cluster access
		return *result, nil
	}

	// Create ConfigMap on MCP cluster
	if err := r.createOrUpdateConfigMap(ctx, config, mcpCluster); err != nil {
		log.Error(err, "Failed to create ConfigMap")
		config.Status.Phase = "Error"
		r.OnboardingCluster.Client().Status().Patch(ctx, config, client.MergeFrom(oldConfig))
		return ctrl.Result{}, err
	}

	// Update status to Ready
	config.Status.Phase = "Ready"
	if err := r.OnboardingCluster.Client().Status().Patch(ctx, config, client.MergeFrom(oldConfig)); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *KubeStateMetricsConfigReconciler) setupClusterAccess(ctx context.Context, req ctrl.Request) (*clusters.Cluster, *ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Reconcile cluster access (finds existing ClusterRequest by name)
	res, err := r.ClusterAccessReconciler.Reconcile(ctx, req)
	if err != nil {
		log.Error(err, "failed to reconcile cluster access for KubeStateMetricsConfig instance")
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
		log.Error(err, "failed to get MCP cluster for KubeStateMetricsConfig instance")
		result := ctrl.Result{RequeueAfter: 30 * time.Second}
		return nil, &result, nil
	}

	return mcpCluster, nil, nil
}

func (r *KubeStateMetricsConfigReconciler) handleDelete(ctx context.Context, req ctrl.Request, config *v1alpha1.KubeStateMetricsConfig) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(config, ConfigMapFinalizer) {
		return ctrl.Result{}, nil
	}

	// Get MCP cluster access
	mcpCluster, result, err := r.setupClusterAccess(ctx, req)
	if err != nil {
		log.Error(err, "Failed to get MCP cluster access for cleanup")
		// Continue with finalizer removal and cluster access cleanup
	} else if result == nil {
		// Only cleanup if we got cluster access
		if err := r.deleteConfigMap(ctx, config, mcpCluster); err != nil {
			log.Error(err, "Failed to delete ConfigMap")
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
	controllerutil.RemoveFinalizer(config, ConfigMapFinalizer)
	if err := r.OnboardingCluster.Client().Update(ctx, config); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *KubeStateMetricsConfigReconciler) createOrUpdateConfigMap(ctx context.Context, config *v1alpha1.KubeStateMetricsConfig, mcpCluster *clusters.Cluster) error {
	log := log.FromContext(ctx)

	// Determine ConfigMap name and namespace
	configMapName := config.Name + configMapSuffix
	configMapNamespace := config.Spec.TargetNamespace
	if configMapNamespace == "" {
		configMapNamespace = defaultTargetNs
	}

	// Create namespace if it doesn't exist
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: configMapNamespace}}
	if _, err := ctrl.CreateOrUpdate(ctx, mcpCluster.Client(), ns, func() error {
		return nil
	}); err != nil {
		return err
	}

	// Build ConfigMap data
	data := make(map[string]string)

	if config.Spec.CustomResourceStateConfig != "" {
		data["custom-resource-state-config.yaml"] = config.Spec.CustomResourceStateConfig
	}

	if config.Spec.Config != "" {
		data["config.yaml"] = config.Spec.Config
	}

	// Add additional configs
	for filename, content := range config.Spec.AdditionalConfigs {
		data[filename] = content
	}

	// Create or update ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: configMapNamespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(ctx, mcpCluster.Client(), configMap, func() error {
		configMap.Labels = map[string]string{
			"app.kubernetes.io/name":       "kube-state-metrics",
			"app.kubernetes.io/component":  "config",
			"app.kubernetes.io/managed-by": "service-provider-ksm",
		}
		configMap.Data = data
		return nil
	}); err != nil {
		return err
	}

	// Update status with ConfigMap details
	config.Status.ConfigMapName = configMapName
	config.Status.ConfigMapNamespace = configMapNamespace

	log.Info("ConfigMap created successfully", "configMap", fmt.Sprintf("%s/%s", configMapNamespace, configMapName))
	return nil
}

func (r *KubeStateMetricsConfigReconciler) deleteConfigMap(ctx context.Context, config *v1alpha1.KubeStateMetricsConfig, mcpCluster *clusters.Cluster) error {
	configMapName := config.Name + configMapSuffix
	configMapNamespace := config.Spec.TargetNamespace
	if configMapNamespace == "" {
		configMapNamespace = defaultTargetNs
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: configMapNamespace,
		},
	}

	if err := mcpCluster.Client().Delete(ctx, configMap); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

func getConfigMCPPermissions() []clustersv1alpha1.PermissionsRequest {
	return []clustersv1alpha1.PermissionsRequest{
		{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"namespaces", "configmaps"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
				},
			},
		},
	}
}

func getConfigMCPRoleRefs() []commonapi.RoleRef {
	return []commonapi.RoleRef{
		{
			Kind: "ClusterRole",
			Name: "cluster-admin",
		},
	}
}
