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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	defaultNamespace    = "observability"
	defaultReplicas     = int32(1)
	componentLabel      = "exporter"
	appName             = "kube-state-metrics"
	imageRegistry       = "crimson-prod.common.repositories.cloud.sap"
	imageRepository     = "kube-state-metrics/kube-state-metrics"
	defaultImageVersion = "v2.18.0"
)

var (
	KSMFinalizer = v1alpha1.GroupVersion.Group + "/finalizer"
)

// KubeStateMetricsReconciler reconciles a KubeStateMetrics object
type KubeStateMetricsReconciler struct {
	PlatformCluster         *clusters.Cluster
	OnboardingCluster       *clusters.Cluster
	ClusterAccessReconciler clusteraccess.Reconciler
	Recorder                record.EventRecorder
	RecieveEventsChannel    <-chan event.GenericEvent
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeStateMetricsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.ClusterAccessReconciler = clusteraccess.NewClusterAccessReconciler(
		r.PlatformCluster.Client(),
		"ksm.services.openmcp.cloud",
	)
	r.ClusterAccessReconciler.
		WithMCPScheme(scheme.MCP).
		WithRetryInterval(10 * time.Second).
		WithMCPPermissions(getMCPPermissions()).
		WithMCPRoleRefs(getMCPRoleRefs()).
		SkipWorkloadCluster()

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.KubeStateMetrics{}).
		Complete(r)
}

// Reconcile reconciles the KubeStateMetrics instance.
func (r *KubeStateMetricsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the KubeStateMetrics instance from the onboarding cluster
	ksm := &v1alpha1.KubeStateMetrics{}
	if err := r.OnboardingCluster.Client().Get(ctx, req.NamespacedName, ksm); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get KubeStateMetrics")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !ksm.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, req, ksm)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(ksm, KSMFinalizer) {
		controllerutil.AddFinalizer(ksm, KSMFinalizer)
		if err := r.OnboardingCluster.Client().Update(ctx, ksm); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Update status to Progressing
	oldKsm := ksm.DeepCopy()
	ksm.Status.Phase = "Progressing"
	ksm.Status.ObservedGeneration = ksm.Generation

	// Setup cluster access (references existing ClusterRequest by name)
	mcpCluster, result, err := r.setupClusterAccess(ctx, req)
	if err != nil {
		log.Error(err, "Failed to setup cluster access")
		ksm.Status.Phase = "Error"
		r.OnboardingCluster.Client().Status().Patch(ctx, ksm, client.MergeFrom(oldKsm))
		return ctrl.Result{}, err
	}
	if result != nil {
		// Requeue to wait for cluster access
		return *result, nil
	}

	// Deploy kube-state-metrics to MCP cluster
	deploymentReady, err := r.deployKubeStateMetrics(ctx, ksm, mcpCluster)
	if err != nil {
		log.Error(err, "Failed to deploy kube-state-metrics")
		ksm.Status.Phase = "Error"
		r.OnboardingCluster.Client().Status().Patch(ctx, ksm, client.MergeFrom(oldKsm))
		return ctrl.Result{}, err
	}

	// Update status based on deployment readiness
	if deploymentReady {
		ksm.Status.Phase = "Ready"
		log.Info("kube-state-metrics is ready")
	} else {
		ksm.Status.Phase = "Progressing"
		log.Info("kube-state-metrics deployment created, waiting for pods to be ready")
	}

	if err := r.OnboardingCluster.Client().Status().Patch(ctx, ksm, client.MergeFrom(oldKsm)); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	// Requeue if not ready yet to check again
	if !deploymentReady {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *KubeStateMetricsReconciler) setupClusterAccess(ctx context.Context, req ctrl.Request) (*clusters.Cluster, *ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Reconcile cluster access (finds existing ClusterRequest by name)
	res, err := r.ClusterAccessReconciler.Reconcile(ctx, req)
	if err != nil {
		log.Error(err, "failed to reconcile cluster access for KubeStateMetrics instance")
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
		log.Error(err, "failed to get MCP cluster for KubeStateMetrics instance")
		result := ctrl.Result{RequeueAfter: 30 * time.Second}
		return nil, &result, nil
	}

	return mcpCluster, nil, nil
}

func (r *KubeStateMetricsReconciler) handleDelete(ctx context.Context, req ctrl.Request, ksm *v1alpha1.KubeStateMetrics) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(ksm, KSMFinalizer) {
		return ctrl.Result{}, nil
	}

	// Get MCP cluster access
	mcpCluster, result, err := r.setupClusterAccess(ctx, req)
	if err != nil {
		log.Error(err, "Failed to get MCP cluster access for cleanup")
		// Continue with finalizer removal and cluster access cleanup
	} else if result == nil {
		// Only cleanup if we got cluster access
		if err := r.cleanupKubeStateMetrics(ctx, ksm, mcpCluster); err != nil {
			log.Error(err, "Failed to cleanup kube-state-metrics")
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
	controllerutil.RemoveFinalizer(ksm, KSMFinalizer)
	if err := r.OnboardingCluster.Client().Update(ctx, ksm); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *KubeStateMetricsReconciler) deployKubeStateMetrics(ctx context.Context, ksm *v1alpha1.KubeStateMetrics, mcpCluster *clusters.Cluster) (bool, error) {
	log := log.FromContext(ctx)

	// Get target namespace
	namespace := ksm.Spec.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	// Build image URL from version
	version := ksm.Spec.Version
	if version == "" {
		version = defaultImageVersion
	}
	imageURL := buildImageURL(version)

	// Create namespace
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	if _, err := ctrl.CreateOrUpdate(ctx, mcpCluster.Client(), ns, func() error {
		return nil
	}); err != nil {
		return false, err
	}

	// Create ServiceAccount
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace}}
	if _, err := ctrl.CreateOrUpdate(ctx, mcpCluster.Client(), sa, func() error {
		sa.Labels = r.buildLabels(ksm)
		sa.AutomountServiceAccountToken = boolPtr(false)
		return nil
	}); err != nil {
		return false, err
	}

	// Create ClusterRole
	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: appName}}
	if _, err := ctrl.CreateOrUpdate(ctx, mcpCluster.Client(), cr, func() error {
		cr.Labels = r.buildLabels(ksm)
		cr.Rules = []rbacv1.PolicyRule{
			{APIGroups: []string{"authentication.k8s.io"}, Resources: []string{"tokenreviews"}, Verbs: []string{"create"}},
			{APIGroups: []string{"authorization.k8s.io"}, Resources: []string{"subjectaccessreviews"}, Verbs: []string{"create"}},
			{APIGroups: []string{"apiextensions.k8s.io"}, Resources: []string{"customresourcedefinitions"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"get", "list", "watch"}},
		}
		return nil
	}); err != nil {
		return false, err
	}

	// Create ClusterRoleBinding
	crb := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: appName}}
	if _, err := ctrl.CreateOrUpdate(ctx, mcpCluster.Client(), crb, func() error {
		crb.Labels = r.buildLabels(ksm)
		crb.RoleRef = rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: appName}
		crb.Subjects = []rbacv1.Subject{{Kind: "ServiceAccount", Name: appName, Namespace: namespace}}
		return nil
	}); err != nil {
		return false, err
	}

	// Resolve config
	var configMapName string
	var hasConfig bool
	if ksm.Spec.ConfigRef != nil {
		configNamespace := ksm.Spec.ConfigRef.Namespace
		if configNamespace == "" {
			configNamespace = ksm.Namespace
		}
		config := &v1alpha1.KubeStateMetricsConfig{}
		if err := r.OnboardingCluster.Client().Get(ctx, client.ObjectKey{Name: ksm.Spec.ConfigRef.Name, Namespace: configNamespace}, config); err != nil {
			return false, fmt.Errorf("KubeStateMetricsConfig not found: %w", err)
		}
		if config.Status.ConfigMapName == "" {
			return false, fmt.Errorf("waiting for KubeStateMetricsConfig to be reconciled")
		}
		configMapName = config.Status.ConfigMapName
		hasConfig = true
		log.Info("Using ConfigMap from KubeStateMetricsConfig", "configMap", configMapName)
	}

	// Create Deployment
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace}}
	if _, err := ctrl.CreateOrUpdate(ctx, mcpCluster.Client(), deployment, func() error {
		replicas := defaultReplicas
		if ksm.Spec.Replicas != nil {
			replicas = *ksm.Spec.Replicas
		}

		labels := r.buildLabels(ksm)
		deployment.Labels = labels

		// Build args
		args := ksm.Spec.Args
		if hasConfig {
			args = append([]string{"--custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state-config.yaml"}, args...)
		}

		container := corev1.Container{
			Name:  appName,
			Image: imageURL,
			Args:  args,
			Ports: []corev1.ContainerPort{
				{ContainerPort: 8080, Name: "http-metrics"},
				{ContainerPort: 8081, Name: "telemetry"},
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler:        corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/livez", Port: intstr.FromString("http-metrics")}},
				InitialDelaySeconds: 15,
				TimeoutSeconds:      5,
				PeriodSeconds:       10,
				SuccessThreshold:    1,
				FailureThreshold:    3,
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler:        corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromString("telemetry")}},
				InitialDelaySeconds: 10,
				TimeoutSeconds:      3,
				PeriodSeconds:       5,
				SuccessThreshold:    1,
				FailureThreshold:    2,
			},
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: boolPtr(false),
				Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				ReadOnlyRootFilesystem:   boolPtr(true),
				RunAsNonRoot:             boolPtr(true),
				RunAsUser:                int64Ptr(65534),
				SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
		}

		if ksm.Spec.Resources != nil {
			container.Resources = *ksm.Spec.Resources
		}

		if hasConfig {
			container.VolumeMounts = []corev1.VolumeMount{{Name: "config", MountPath: "/etc/kube-state-metrics/"}}
		}

		podSpec := corev1.PodSpec{
			ServiceAccountName:            appName,
			AutomountServiceAccountToken:  boolPtr(true),
			Containers:                    []corev1.Container{container},
			NodeSelector:                  map[string]string{"kubernetes.io/os": "linux"},
			TerminationGracePeriodSeconds: int64Ptr(30),
		}

		if ksm.Spec.NodeSelector != nil {
			for k, v := range ksm.Spec.NodeSelector {
				podSpec.NodeSelector[k] = v
			}
		}

		if len(ksm.Spec.ImagePullSecrets) > 0 {
			podSpec.ImagePullSecrets = ksm.Spec.ImagePullSecrets
		}

		if ksm.Spec.SecurityContext != nil {
			podSpec.SecurityContext = ksm.Spec.SecurityContext
		}

		if hasConfig {
			podSpec.Volumes = []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configMapName}}}}}
		}

		// Production-ready deployment strategy
		maxUnavailable := intstr.FromInt(0)
		maxSurge := intstr.FromInt(1)
		minReadySeconds := int32(10)
		progressDeadlineSeconds := int32(300)
		revisionHistoryLimit := int32(10)

		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": appName}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: podSpec},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &maxUnavailable,
					MaxSurge:       &maxSurge,
				},
			},
			MinReadySeconds:         minReadySeconds,
			ProgressDeadlineSeconds: &progressDeadlineSeconds,
			RevisionHistoryLimit:    &revisionHistoryLimit,
		}
		return nil
	}); err != nil {
		return false, err
	}

	// Create Service
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace}}
	if _, err := ctrl.CreateOrUpdate(ctx, mcpCluster.Client(), svc, func() error {
		svc.Labels = r.buildLabels(ksm)
		svc.Spec = corev1.ServiceSpec{
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{Name: "http-metrics", Port: 8080, TargetPort: intstr.FromString("http-metrics")},
				{Name: "telemetry", Port: 8081, TargetPort: intstr.FromString("telemetry")},
			},
			Selector: map[string]string{"app.kubernetes.io/name": appName},
		}
		return nil
	}); err != nil {
		return false, err
	}

	// Create PodDisruptionBudget for production readiness
	pdb := &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace}}
	if _, err := ctrl.CreateOrUpdate(ctx, mcpCluster.Client(), pdb, func() error {
		minAvailable := intstr.FromInt(1)
		pdb.Labels = r.buildLabels(ksm)
		pdb.Spec = policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app.kubernetes.io/name": appName},
			},
		}
		return nil
	}); err != nil {
		return false, err
	}

	// Check deployment status (non-blocking)
	if err := mcpCluster.Client().Get(ctx, client.ObjectKeyFromObject(deployment), deployment); err != nil {
		return false, err
	}

	// Return readiness status without erroring
	if deployment.Status.ReadyReplicas == deployment.Status.Replicas && deployment.Status.Replicas > 0 {
		log.Info("kube-state-metrics deployment is ready", "replicas", deployment.Status.ReadyReplicas)
		return true, nil
	}

	log.Info("kube-state-metrics deployment not yet ready", "ready", deployment.Status.ReadyReplicas, "desired", deployment.Status.Replicas)
	return false, nil
}

func (r *KubeStateMetricsReconciler) cleanupKubeStateMetrics(ctx context.Context, ksm *v1alpha1.KubeStateMetrics, mcpCluster *clusters.Cluster) error {
	namespace := ksm.Spec.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	resources := []client.Object{
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace}},
		&policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: appName}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: appName}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace}},
	}

	for _, resource := range resources {
		if err := mcpCluster.Client().Delete(ctx, resource); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (r *KubeStateMetricsReconciler) buildLabels(obj *v1alpha1.KubeStateMetrics) map[string]string {
	version := obj.Spec.Version
	if version == "" {
		version = defaultImageVersion
	}
	return map[string]string{
		"app.kubernetes.io/name":      appName,
		"app.kubernetes.io/component": componentLabel,
		"app.kubernetes.io/version":   version,
	}
}

func boolPtr(b bool) *bool    { return &b }
func int64Ptr(i int64) *int64 { return &i }

// buildImageURL constructs the full image URL from the SAP internal registry
func buildImageURL(version string) string {
	return imageRegistry + "/" + imageRepository + ":" + version
}

func getMCPPermissions() []clustersv1alpha1.PermissionsRequest {
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

func getMCPRoleRefs() []commonapi.RoleRef {
	return []commonapi.RoleRef{
		{
			Kind: "ClusterRole",
			Name: "cluster-admin",
		},
	}
}
