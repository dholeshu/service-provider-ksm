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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1alpha1 "github.com/dholeshu/service-provider-ksm/api/v1alpha1"
	spruntime "github.com/dholeshu/service-provider-ksm/pkg/runtime"
	"github.com/openmcp-project/controller-utils/pkg/clusters"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	configMapSuffix = "-ksm-config"
)

// KubeStateMetricsConfigReconciler reconciles a KubeStateMetricsConfig object
type KubeStateMetricsConfigReconciler struct {
	// OnboardingCluster is the cluster where this controller watches KubeStateMetricsConfig resources
	OnboardingCluster *clusters.Cluster
	// PlatformCluster is the cluster where this controller is deployed and configured
	PlatformCluster *clusters.Cluster
	// PodNamespace is the namespace where this controller is deployed in
	PodNamespace string
}

// CreateOrUpdate is called on every add or update event
func (r *KubeStateMetricsConfigReconciler) CreateOrUpdate(ctx context.Context, configObj *apiv1alpha1.KubeStateMetricsConfig, _ *apiv1alpha1.ProviderConfig, clusters spruntime.ClusterContext) (ctrl.Result, error) {
	l := logf.FromContext(ctx)
	spruntime.StatusProgressing(configObj, "Reconciling", "Reconciling KubeStateMetricsConfig")

	// Determine ConfigMap name and namespace
	configMapName := configObj.Name + configMapSuffix
	configMapNamespace := configObj.Namespace
	if configMapNamespace == "" {
		configMapNamespace = defaultNamespace
	}

	// Create namespace if it doesn't exist
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: configMapNamespace,
		},
	}
	if _, err := ctrl.CreateOrUpdate(ctx, clusters.MCPCluster.Client(), ns, func() error {
		return nil
	}); err != nil {
		l.Error(err, "failed to create namespace")
		spruntime.StatusProgressing(configObj, "NamespaceCreationFailed", err.Error())
		return ctrl.Result{}, err
	}

	// Build ConfigMap data
	data := make(map[string]string)

	if configObj.Spec.CustomResourceStateConfig != "" {
		data["custom-resource-state-config.yaml"] = configObj.Spec.CustomResourceStateConfig
	}

	if configObj.Spec.Config != "" {
		data["config.yaml"] = configObj.Spec.Config
	}

	// Add additional configs
	for filename, content := range configObj.Spec.AdditionalConfigs {
		data[filename] = content
	}

	// Create or update ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: configMapNamespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(ctx, clusters.MCPCluster.Client(), configMap, func() error {
		configMap.Labels = map[string]string{
			"app.kubernetes.io/name":       "kube-state-metrics",
			"app.kubernetes.io/component":  "config",
			"app.kubernetes.io/managed-by": "service-provider-ksm",
		}
		configMap.Data = data
		return nil
	}); err != nil {
		l.Error(err, "failed to create or update ConfigMap")
		spruntime.StatusProgressing(configObj, "ConfigMapFailed", err.Error())
		return ctrl.Result{}, err
	}

	// Update status
	configObj.Status.ConfigMapName = configMapName
	configObj.Status.ConfigMapNamespace = configMapNamespace
	spruntime.StatusReady(configObj)

	l.Info("KubeStateMetricsConfig reconciled successfully", "configMap", fmt.Sprintf("%s/%s", configMapNamespace, configMapName))
	return ctrl.Result{}, nil
}

// Delete is called on every delete event
func (r *KubeStateMetricsConfigReconciler) Delete(ctx context.Context, configObj *apiv1alpha1.KubeStateMetricsConfig, _ *apiv1alpha1.ProviderConfig, clusters spruntime.ClusterContext) (ctrl.Result, error) {
	l := logf.FromContext(ctx)
	spruntime.StatusTerminating(configObj)

	configMapName := configObj.Name + configMapSuffix
	configMapNamespace := configObj.Namespace
	if configMapNamespace == "" {
		configMapNamespace = defaultNamespace
	}

	// Delete ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: configMapNamespace,
		},
	}

	if err := clusters.MCPCluster.Client().Delete(ctx, configMap); err != nil {
		if !errors.IsNotFound(err) {
			l.Error(err, "failed to delete ConfigMap")
			return ctrl.Result{}, err
		}
	}

	// Check if ConfigMap still exists
	if err := clusters.MCPCluster.Client().Get(ctx, client.ObjectKeyFromObject(configMap), configMap); err != nil {
		if errors.IsNotFound(err) {
			// ConfigMap is deleted
			return reconcile.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// ConfigMap still exists, requeue
	return ctrl.Result{RequeueAfter: time.Second * 10}, nil
}
