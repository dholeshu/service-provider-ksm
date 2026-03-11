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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openmcp-project/controller-utils/pkg/clusters"

	v1alpha1 "github.com/dholeshu/service-provider-ksm/api/v1alpha1"
	spruntime "github.com/dholeshu/service-provider-ksm/pkg/runtime"
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
	PlatformCluster      *clusters.Cluster
	OnboardingCluster    *clusters.Cluster
	Recorder             record.EventRecorder
	RecieveEventsChannel <-chan event.GenericEvent
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeStateMetricsConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.KubeStateMetricsConfig{}).
		Complete(r)
}

// Reconcile reconciles the KubeStateMetricsConfig instance.
// The config controller validates and stores configuration on the onboarding cluster.
// The KubeStateMetrics controller handles deploying the ConfigMap to the MCP cluster.
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
		return r.handleDelete(ctx, config)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(config, ConfigMapFinalizer) {
		controllerutil.AddFinalizer(config, ConfigMapFinalizer)
		if err := r.OnboardingCluster.Client().Update(ctx, config); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Validate config and set status to Ready
	oldConfig := config.DeepCopy()
	spruntime.StatusProgressing(config, "Reconciling", "Validating configuration")

	// Determine ConfigMap name that the KubeStateMetrics controller will use
	configMapName := config.Name + configMapSuffix
	config.Status.ConfigMapName = configMapName
	configMapNamespace := config.Spec.TargetNamespace
	if configMapNamespace == "" {
		configMapNamespace = defaultTargetNs
	}
	config.Status.ConfigMapNamespace = configMapNamespace

	// Mark as Ready — the KubeStateMetrics controller will deploy the ConfigMap to MCP
	spruntime.StatusReady(config)
	if err := r.OnboardingCluster.Client().Status().Patch(ctx, config, client.MergeFrom(oldConfig)); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("KubeStateMetricsConfig reconciled successfully", "configMapName", configMapName)
	return ctrl.Result{}, nil
}

func (r *KubeStateMetricsConfigReconciler) handleDelete(ctx context.Context, config *v1alpha1.KubeStateMetricsConfig) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(config, ConfigMapFinalizer) {
		return ctrl.Result{}, nil
	}

	// Update status to Terminating
	spruntime.StatusTerminating(config)
	if err := r.OnboardingCluster.Client().Status().Update(ctx, config); err != nil {
		log.Error(err, "Failed to update status to terminating")
		// Continue with deletion anyway
	}

	// Remove finalizer — the KubeStateMetrics controller handles MCP cleanup
	controllerutil.RemoveFinalizer(config, ConfigMapFinalizer)
	if err := r.OnboardingCluster.Client().Update(ctx, config); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
