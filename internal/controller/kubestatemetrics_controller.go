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
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1alpha1 "github.com/dholeshu/service-provider-ksm/api/v1alpha1"
	spruntime "github.com/dholeshu/service-provider-ksm/pkg/runtime"
	"github.com/openmcp-project/controller-utils/pkg/clusters"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultNamespace = "observability"
	defaultReplicas  = int32(1)
	componentLabel   = "exporter"
	appName          = "kube-state-metrics"
)

// KubeStateMetricsReconciler reconciles a KubeStateMetrics object
type KubeStateMetricsReconciler struct {
	// OnboardingCluster is the cluster where this controller watches KubeStateMetrics resources and reacts to their changes.
	OnboardingCluster *clusters.Cluster
	// PlatformCluster is the cluster where this controller is deployed and configured.
	PlatformCluster *clusters.Cluster
	// PodNamespace is the namespace where this controller is deployed in.
	PodNamespace string
}

// CreateOrUpdate is called on every add or update event
func (r *KubeStateMetricsReconciler) CreateOrUpdate(ctx context.Context, svcobj *apiv1alpha1.KubeStateMetrics, _ *apiv1alpha1.ProviderConfig, clusters spruntime.ClusterContext) (ctrl.Result, error) {
	l := logf.FromContext(ctx)
	spruntime.StatusProgressing(svcobj, "Reconciling", "Reconciling kube-state-metrics resources")

	// Get target namespace
	namespace := svcobj.Spec.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	// Create namespace if it doesn't exist
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	if _, err := ctrl.CreateOrUpdate(ctx, clusters.MCPCluster.Client(), ns, func() error {
		return nil
	}); err != nil {
		l.Error(err, "failed to create namespace")
		spruntime.StatusProgressing(svcobj, "NamespaceCreationFailed", err.Error())
		return ctrl.Result{}, err
	}

	// Create ServiceAccount
	sa := r.buildServiceAccount(svcobj, namespace)
	if _, err := ctrl.CreateOrUpdate(ctx, clusters.MCPCluster.Client(), sa, func() error {
		r.applyServiceAccountSpec(sa, svcobj, namespace)
		return nil
	}); err != nil {
		l.Error(err, "failed to create or update ServiceAccount")
		spruntime.StatusProgressing(svcobj, "ServiceAccountFailed", err.Error())
		return ctrl.Result{}, err
	}

	// Create ClusterRole
	cr := r.buildClusterRole(svcobj)
	if _, err := ctrl.CreateOrUpdate(ctx, clusters.MCPCluster.Client(), cr, func() error {
		r.applyClusterRoleSpec(cr, svcobj)
		return nil
	}); err != nil {
		l.Error(err, "failed to create or update ClusterRole")
		spruntime.StatusProgressing(svcobj, "ClusterRoleFailed", err.Error())
		return ctrl.Result{}, err
	}

	// Create ClusterRoleBinding
	crb := r.buildClusterRoleBinding(svcobj, namespace)
	if _, err := ctrl.CreateOrUpdate(ctx, clusters.MCPCluster.Client(), crb, func() error {
		r.applyClusterRoleBindingSpec(crb, svcobj, namespace)
		return nil
	}); err != nil {
		l.Error(err, "failed to create or update ClusterRoleBinding")
		spruntime.StatusProgressing(svcobj, "ClusterRoleBindingFailed", err.Error())
		return ctrl.Result{}, err
	}

	// Resolve configuration - either from ConfigRef or inline (deprecated)
	var configMapName string
	var hasConfig bool
	var ksmConfig *apiv1alpha1.KubeStateMetricsConfig

	if svcobj.Spec.ConfigRef != nil {
		// Get the referenced KubeStateMetricsConfig
		configNamespace := svcobj.Spec.ConfigRef.Namespace
		if configNamespace == "" {
			configNamespace = svcobj.Namespace
		}

		config := &apiv1alpha1.KubeStateMetricsConfig{}
		configKey := client.ObjectKey{
			Name:      svcobj.Spec.ConfigRef.Name,
			Namespace: configNamespace,
		}

		if err := r.OnboardingCluster.Client().Get(ctx, configKey, config); err != nil {
			l.Error(err, "failed to get referenced KubeStateMetricsConfig", "configRef", svcobj.Spec.ConfigRef.Name)
			spruntime.StatusProgressing(svcobj, "ConfigRefNotFound", fmt.Sprintf("KubeStateMetricsConfig %s/%s not found", configNamespace, svcobj.Spec.ConfigRef.Name))
			return ctrl.Result{RequeueAfter: time.Second * 30}, err
		}

		// Check if the config has been reconciled and has a ConfigMap
		if config.Status.ConfigMapName == "" {
			l.Info("KubeStateMetricsConfig not yet reconciled, waiting", "config", config.Name)
			spruntime.StatusProgressing(svcobj, "WaitingForConfig", "Waiting for KubeStateMetricsConfig to be reconciled")
			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		}

		configMapName = config.Status.ConfigMapName
		ksmConfig = config
		hasConfig = true
		l.Info("Using ConfigMap from KubeStateMetricsConfig", "configMap", fmt.Sprintf("%s/%s", config.Status.ConfigMapNamespace, configMapName))
	}

	// Create Deployment
	deployment := r.buildDeployment(svcobj, namespace)
	if _, err := ctrl.CreateOrUpdate(ctx, clusters.MCPCluster.Client(), deployment, func() error {
		r.applyDeploymentSpec(deployment, svcobj, namespace, configMapName, hasConfig, ksmConfig)
		return nil
	}); err != nil {
		l.Error(err, "failed to create or update Deployment")
		spruntime.StatusProgressing(svcobj, "DeploymentFailed", err.Error())
		return ctrl.Result{}, err
	}

	// Create Service
	service := r.buildService(svcobj, namespace)
	if _, err := ctrl.CreateOrUpdate(ctx, clusters.MCPCluster.Client(), service, func() error {
		r.applyServiceSpec(service, svcobj, namespace)
		return nil
	}); err != nil {
		l.Error(err, "failed to create or update Service")
		spruntime.StatusProgressing(svcobj, "ServiceFailed", err.Error())
		return ctrl.Result{}, err
	}

	// Check deployment status
	if err := clusters.MCPCluster.Client().Get(ctx, client.ObjectKeyFromObject(deployment), deployment); err != nil {
		l.Error(err, "failed to get deployment status")
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	if deployment.Status.ReadyReplicas == deployment.Status.Replicas && deployment.Status.Replicas > 0 {
		spruntime.StatusReady(svcobj)
		l.Info("kube-state-metrics deployment is ready")
	} else {
		spruntime.StatusProgressing(svcobj, "DeploymentNotReady", fmt.Sprintf("Deployment has %d/%d ready replicas", deployment.Status.ReadyReplicas, deployment.Status.Replicas))
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	return ctrl.Result{}, nil
}

// Delete is called on every delete event
func (r *KubeStateMetricsReconciler) Delete(ctx context.Context, obj *apiv1alpha1.KubeStateMetrics, _ *apiv1alpha1.ProviderConfig, clusters spruntime.ClusterContext) (ctrl.Result, error) {
	l := logf.FromContext(ctx)
	spruntime.StatusTerminating(obj)

	namespace := obj.Spec.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	// Delete in reverse order of creation
	// Note: ConfigMap is managed by KubeStateMetricsConfig controller, not deleted here
	resources := []client.Object{
		r.buildService(obj, namespace),
		r.buildDeployment(obj, namespace),
		r.buildClusterRoleBinding(obj, namespace),
		r.buildClusterRole(obj),
		r.buildServiceAccount(obj, namespace),
	}

	allDeleted := true
	for _, resource := range resources {
		if err := clusters.MCPCluster.Client().Delete(ctx, resource); err != nil {
			if !errors.IsNotFound(err) {
				l.Error(err, "failed to delete resource", "kind", resource.GetObjectKind().GroupVersionKind().Kind, "name", resource.GetName())
				return ctrl.Result{}, err
			}
		} else {
			// Check if resource still exists
			if err := clusters.MCPCluster.Client().Get(ctx, client.ObjectKeyFromObject(resource), resource); err == nil {
				allDeleted = false
			}
		}
	}

	if !allDeleted {
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	return reconcile.Result{}, nil
}

func (r *KubeStateMetricsReconciler) buildServiceAccount(obj *apiv1alpha1.KubeStateMetrics, namespace string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: namespace,
		},
	}
}

func (r *KubeStateMetricsReconciler) applyServiceAccountSpec(sa *corev1.ServiceAccount, obj *apiv1alpha1.KubeStateMetrics, namespace string) {
	sa.Labels = r.buildLabels(obj)
	sa.AutomountServiceAccountToken = boolPtr(false)
}

func (r *KubeStateMetricsReconciler) buildClusterRole(obj *apiv1alpha1.KubeStateMetrics) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: appName,
		},
	}
}

func (r *KubeStateMetricsReconciler) applyClusterRoleSpec(cr *rbacv1.ClusterRole, obj *apiv1alpha1.KubeStateMetrics) {
	cr.Labels = r.buildLabels(obj)
	cr.Rules = []rbacv1.PolicyRule{
		{
			APIGroups: []string{"authentication.k8s.io"},
			Resources: []string{"tokenreviews"},
			Verbs:     []string{"create"},
		},
		{
			APIGroups: []string{"authorization.k8s.io"},
			Resources: []string{"subjectaccessreviews"},
			Verbs:     []string{"create"},
		},
		{
			APIGroups: []string{"apiextensions.k8s.io"},
			Resources: []string{"customresourcedefinitions"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"*"},
			Resources: []string{"*"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}
}

func (r *KubeStateMetricsReconciler) buildClusterRoleBinding(obj *apiv1alpha1.KubeStateMetrics, namespace string) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: appName,
		},
	}
}

func (r *KubeStateMetricsReconciler) applyClusterRoleBindingSpec(crb *rbacv1.ClusterRoleBinding, obj *apiv1alpha1.KubeStateMetrics, namespace string) {
	crb.Labels = r.buildLabels(obj)
	crb.RoleRef = rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "ClusterRole",
		Name:     appName,
	}
	crb.Subjects = []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      appName,
			Namespace: namespace,
		},
	}
}

func (r *KubeStateMetricsReconciler) buildDeployment(obj *apiv1alpha1.KubeStateMetrics, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: namespace,
		},
	}
}

func (r *KubeStateMetricsReconciler) applyDeploymentSpec(deployment *appsv1.Deployment, obj *apiv1alpha1.KubeStateMetrics, namespace string, configMapName string, hasConfig bool, ksmConfig *apiv1alpha1.KubeStateMetricsConfig) {
	replicas := defaultReplicas
	if obj.Spec.Replicas != nil {
		replicas = *obj.Spec.Replicas
	}

	labels := r.buildLabels(obj)
	deployment.Labels = labels

	// Build container args
	args := []string{}

	// Determine if we should use custom-resource-state-only mode
	// Default to true if specified, but override if we have standard config
	customResourceStateOnly := true
	if obj.Spec.CustomResourceStateOnly != nil {
		customResourceStateOnly = *obj.Spec.CustomResourceStateOnly
	}

	// If we have standard config, we're not in custom-resource-state-only mode
	if hasConfig && ksmConfig != nil && ksmConfig.Spec.Config != "" {
		customResourceStateOnly = false
	}

	if customResourceStateOnly {
		args = append(args, "--custom-resource-state-only=true")
	}

	// Add config file args based on what's available in the KubeStateMetricsConfig
	if hasConfig && ksmConfig != nil {
		// Add custom resource state config if present
		if ksmConfig.Spec.CustomResourceStateConfig != "" {
			args = append(args, "--custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state-config.yaml")
		}

		// Add standard config if present
		if ksmConfig.Spec.Config != "" {
			args = append(args, "--config=/etc/kube-state-metrics/config.yaml")
		}
	}

	// Add additional args from spec
	args = append(args, obj.Spec.Args...)

	// Build container
	container := corev1.Container{
		Name:  appName,
		Image: obj.Spec.Image,
		Args:  args,
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: 8080,
				Name:          "http-metrics",
			},
			{
				ContainerPort: 8081,
				Name:          "telemetry",
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/livez",
					Port: intstr.FromString("http-metrics"),
				},
			},
			InitialDelaySeconds: 5,
			TimeoutSeconds:      5,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/readyz",
					Port: intstr.FromString("telemetry"),
				},
			},
			InitialDelaySeconds: 5,
			TimeoutSeconds:      5,
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: boolPtr(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			ReadOnlyRootFilesystem: boolPtr(true),
			RunAsNonRoot:           boolPtr(true),
			RunAsUser:              int64Ptr(65534),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
	}

	// Add resource requirements if specified
	if obj.Spec.Resources != nil {
		container.Resources = *obj.Spec.Resources
	}

	// Add volume mount if config is provided (via ConfigRef)
	if hasConfig {
		container.VolumeMounts = []corev1.VolumeMount{
			{
				Name:      "config",
				MountPath: "/etc/kube-state-metrics/",
			},
		}
	}

	// Build pod template
	podTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName:           appName,
			AutomountServiceAccountToken: boolPtr(true),
			Containers:                   []corev1.Container{container},
			NodeSelector: map[string]string{
				"kubernetes.io/os": "linux",
			},
		},
	}

	// Add custom node selector if specified
	if obj.Spec.NodeSelector != nil {
		for k, v := range obj.Spec.NodeSelector {
			podTemplate.Spec.NodeSelector[k] = v
		}
	}

	// Add image pull secrets if specified
	if len(obj.Spec.ImagePullSecrets) > 0 {
		podTemplate.Spec.ImagePullSecrets = obj.Spec.ImagePullSecrets
	}

	// Add security context if specified
	if obj.Spec.SecurityContext != nil {
		podTemplate.Spec.SecurityContext = obj.Spec.SecurityContext
	}

	// Add volume if config is provided (via ConfigRef)
	if hasConfig {
		podTemplate.Spec.Volumes = []corev1.Volume{
			{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: configMapName,
						},
					},
				},
			},
		}
	}

	deployment.Spec = appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app.kubernetes.io/name": appName,
			},
		},
		Template: podTemplate,
	}
}

func (r *KubeStateMetricsReconciler) buildService(obj *apiv1alpha1.KubeStateMetrics, namespace string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: namespace,
		},
	}
}

func (r *KubeStateMetricsReconciler) applyServiceSpec(service *corev1.Service, obj *apiv1alpha1.KubeStateMetrics, namespace string) {
	service.Labels = r.buildLabels(obj)
	service.Spec = corev1.ServiceSpec{
		ClusterIP: "None",
		Ports: []corev1.ServicePort{
			{
				Name:       "http-metrics",
				Port:       8080,
				TargetPort: intstr.FromString("http-metrics"),
			},
			{
				Name:       "telemetry",
				Port:       8081,
				TargetPort: intstr.FromString("telemetry"),
			},
		},
		Selector: map[string]string{
			"app.kubernetes.io/name": appName,
		},
	}
}

func (r *KubeStateMetricsReconciler) buildLabels(obj *apiv1alpha1.KubeStateMetrics) map[string]string {
	// Extract version from image tag (e.g., "image:v2.18.0" -> "v2.18.0")
	version := extractVersionFromImage(obj.Spec.Image)

	return map[string]string{
		"app.kubernetes.io/name":      appName,
		"app.kubernetes.io/component": componentLabel,
		"app.kubernetes.io/version":   version,
	}
}

// extractVersionFromImage extracts the version tag from the container image
// e.g., "registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0" -> "v2.18.0"
func extractVersionFromImage(image string) string {
	// Split by : to get the tag
	parts := strings.Split(image, ":")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return "unknown"
}

func boolPtr(b bool) *bool {
	return &b
}

func int64Ptr(i int64) *int64 {
	return &i
}
