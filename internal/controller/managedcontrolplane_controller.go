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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	apiv1alpha1 "github.com/dholeshu/service-provider-ksm/api/v1alpha1"
	mcpv1alpha1 "github.com/openmcp-project/mcp-operator/api/core/v1alpha1"
)

const (
	ksmFinalizerName = "ksm.services.openmcp.cloud/kubestatemetrics"
)

// ManagedControlPlaneReconciler watches ManagedControlPlane resources and creates KubeStateMetrics service resources
type ManagedControlPlaneReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.openmcp.cloud,resources=managedcontrolplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core.openmcp.cloud,resources=managedcontrolplanes/status,verbs=get
// +kubebuilder:rbac:groups=core.openmcp.cloud,resources=managedcontrolplanes/finalizers,verbs=update
// +kubebuilder:rbac:groups=ksm.services.openmcp.cloud,resources=kubestatemetrics,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ksm.services.openmcp.cloud,resources=kubestatemetrics/status,verbs=get;update;patch

func (r *ManagedControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	// Fetch the ManagedControlPlane resource
	mcp := &mcpv1alpha1.ManagedControlPlane{}
	if err := r.Get(ctx, req.NamespacedName, mcp); err != nil {
		if errors.IsNotFound(err) {
			// Resource deleted, nothing to do
			return ctrl.Result{}, nil
		}
		l.Error(err, "failed to get ManagedControlPlane")
		return ctrl.Result{}, err
	}

	// Check if KubeStateMetrics component is configured
	ksmConfig := mcp.Spec.Components.KubeStateMetrics
	if ksmConfig == nil {
		// KSM not configured, check if we need to clean up existing resources
		return r.cleanupKSMResources(ctx, mcp)
	}

	// Handle deletion
	if !mcp.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, mcp)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(mcp, ksmFinalizerName) {
		controllerutil.AddFinalizer(mcp, ksmFinalizerName)
		if err := r.Update(ctx, mcp); err != nil {
			l.Error(err, "failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Create or update KubeStateMetrics service resource
	return r.reconcileKSMResource(ctx, mcp, ksmConfig)
}

func (r *ManagedControlPlaneReconciler) reconcileKSMResource(ctx context.Context, mcp *mcpv1alpha1.ManagedControlPlane, ksmConfig *mcpv1alpha1.KubeStateMetricsConfig) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	// Build KubeStateMetrics resource name from MCP
	ksmName := fmt.Sprintf("%s-ksm", mcp.Name)

	// Create KubeStateMetrics resource
	ksm := &apiv1alpha1.KubeStateMetrics{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ksmName,
			Namespace: mcp.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "kube-state-metrics",
				"app.kubernetes.io/managed-by": "service-provider-ksm",
				"openmcp.cloud/mcp":            mcp.Name,
			},
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ksm, func() error {
		// Set MCP as owner reference
		if err := controllerutil.SetControllerReference(mcp, ksm, r.Scheme); err != nil {
			return err
		}

		// Use version from config or default
		version := "v2.18.0" // Default version
		if ksmConfig.Version != "" {
			version = ksmConfig.Version
		}

		// Build full image URL
		image := fmt.Sprintf("crimson-prod.common.repositories.cloud.sap/kube-state-metrics/kube-state-metrics:%s", version)

		ksm.Spec = apiv1alpha1.KubeStateMetricsSpec{
			Image:                   image,
			Namespace:               "observability",
			CustomResourceStateOnly: boolPtr(true),
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			},
		}

		return nil
	})

	if err != nil {
		l.Error(err, "failed to create or update KubeStateMetrics resource")
		return ctrl.Result{}, err
	}

	l.Info("KubeStateMetrics resource reconciled successfully", "name", ksmName)
	return ctrl.Result{}, nil
}

func (r *ManagedControlPlaneReconciler) cleanupKSMResources(ctx context.Context, mcp *mcpv1alpha1.ManagedControlPlane) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	// Check if finalizer exists
	if !controllerutil.ContainsFinalizer(mcp, ksmFinalizerName) {
		return ctrl.Result{}, nil
	}

	// Delete KubeStateMetrics resource if it exists
	ksmName := fmt.Sprintf("%s-ksm", mcp.Name)
	ksm := &apiv1alpha1.KubeStateMetrics{}
	err := r.Get(ctx, types.NamespacedName{Name: ksmName, Namespace: mcp.Namespace}, ksm)
	if err == nil {
		// Resource exists, delete it
		if err := r.Delete(ctx, ksm); err != nil && !errors.IsNotFound(err) {
			l.Error(err, "failed to delete KubeStateMetrics resource")
			return ctrl.Result{}, err
		}
	} else if !errors.IsNotFound(err) {
		l.Error(err, "failed to get KubeStateMetrics resource")
		return ctrl.Result{}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(mcp, ksmFinalizerName)
	if err := r.Update(ctx, mcp); err != nil {
		l.Error(err, "failed to remove finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ManagedControlPlaneReconciler) handleDeletion(ctx context.Context, mcp *mcpv1alpha1.ManagedControlPlane) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	// Check if finalizer exists
	if !controllerutil.ContainsFinalizer(mcp, ksmFinalizerName) {
		return ctrl.Result{}, nil
	}

	// Delete KubeStateMetrics resource
	ksmName := fmt.Sprintf("%s-ksm", mcp.Name)
	ksm := &apiv1alpha1.KubeStateMetrics{}
	err := r.Get(ctx, types.NamespacedName{Name: ksmName, Namespace: mcp.Namespace}, ksm)
	if err == nil {
		// Resource exists, delete it
		if err := r.Delete(ctx, ksm); err != nil && !errors.IsNotFound(err) {
			l.Error(err, "failed to delete KubeStateMetrics resource")
			return ctrl.Result{}, err
		}
		// Requeue to wait for deletion
		l.Info("Waiting for KubeStateMetrics resource deletion")
		return ctrl.Result{Requeue: true}, nil
	} else if !errors.IsNotFound(err) {
		l.Error(err, "failed to get KubeStateMetrics resource")
		return ctrl.Result{}, err
	}

	// Resource deleted, remove finalizer
	controllerutil.RemoveFinalizer(mcp, ksmFinalizerName)
	if err := r.Update(ctx, mcp); err != nil {
		l.Error(err, "failed to remove finalizer")
		return ctrl.Result{}, err
	}

	l.Info("KubeStateMetrics resources cleaned up successfully")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.ManagedControlPlane{}).
		Owns(&apiv1alpha1.KubeStateMetrics{}).
		Complete(r)
}
