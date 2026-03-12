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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigReference references a KubeStateMetricsConfig resource
type ConfigReference struct {
	// Name is the name of the KubeStateMetricsConfig resource
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the KubeStateMetricsConfig resource
	// If not specified, uses the same namespace as the KubeStateMetrics resource
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// KubeStateMetricsSpec defines the desired state of KubeStateMetrics
type KubeStateMetricsSpec struct {
	// Image specifies the kube-state-metrics image to deploy
	// Example: crimson-prod.common.repositories.cloud.sap/kube-state-metrics/kube-state-metrics:v2.18.0
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:example="crimson-prod.common.repositories.cloud.sap/kube-state-metrics/kube-state-metrics:v2.18.0"
	Image string `json:"image"`

	// Namespace specifies the target namespace for kube-state-metrics deployment
	// +kubebuilder:default="observability"
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Replicas specifies the number of kube-state-metrics replicas
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// ImagePullSecrets specifies the image pull secrets for the kube-state-metrics deployment
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Resources defines resource requests and limits for the kube-state-metrics container
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// ConfigRef references a KubeStateMetricsConfig resource containing the configuration
	// +optional
	ConfigRef *ConfigReference `json:"configRef,omitempty"`

	// CustomResourceStateOnly when true, only monitors custom resources (not built-in Kubernetes resources)
	// +kubebuilder:default=true
	// +optional
	CustomResourceStateOnly *bool `json:"customResourceStateOnly,omitempty"`

	// Args specifies additional arguments to pass to kube-state-metrics
	// +optional
	Args []string `json:"args,omitempty"`

	// NodeSelector specifies node selector for pod scheduling
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// SecurityContext defines the security context for the kube-state-metrics pod
	// +optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`
}

// KubeStateMetricsStatus defines the observed state of KubeStateMetrics.
type KubeStateMetricsStatus struct {
	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the KubeStateMetrics resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the generation of this resource that was last reconciled by the controller.
	ObservedGeneration int64 `json:"observedGeneration"`
	// Phase is the current phase of the resource.
	Phase string `json:"phase"`

	// ConfigSource indicates the source of the active configuration.
	// "mcp" means a ConfigMap named kube-state-metrics-config was found on the MCP cluster.
	// "onboarding" means the configuration was resolved from a KubeStateMetricsConfig resource via configRef.
	// Empty means no configuration is active.
	// +optional
	ConfigSource string `json:"configSource,omitempty"`

	// ConfigHash is the SHA-256 hash of the active ConfigMap data.
	// Changes to this value trigger a rolling restart of the kube-state-metrics pods.
	// +optional
	ConfigHash string `json:"configHash,omitempty"`
}

// KubeStateMetrics is the Schema for the kubestatemetricss API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.phase`,name="Phase",type=string
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=onboarding"
type KubeStateMetrics struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of KubeStateMetrics
	// +required
	Spec KubeStateMetricsSpec `json:"spec"`

	// status defines the observed state of KubeStateMetrics
	// +optional
	Status KubeStateMetricsStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// KubeStateMetricsList contains a list of KubeStateMetrics
type KubeStateMetricsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeStateMetrics `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeStateMetrics{}, &KubeStateMetricsList{})
}

// Finalizer returns the finalizer string for the KubeStateMetrics resource
func (o *KubeStateMetrics) Finalizer() string {
	return GroupVersion.Group + "/finalizer"
}

// GetStatus returns the status of the KubeStateMetrics resource
func (o *KubeStateMetrics) GetStatus() any {
	return o.Status
}

// GetConditions returns the conditions of the KubeStateMetrics resource
func (o *KubeStateMetrics) GetConditions() *[]metav1.Condition {
	return &o.Status.Conditions
}

// SetPhase sets the phase of the KubeStateMetrics resource status
func (o *KubeStateMetrics) SetPhase(phase string) {
	o.Status.Phase = phase
}

// SetObservedGeneration sets the observed generation of the KubeStateMetrics resource
func (o *KubeStateMetrics) SetObservedGeneration(gen int64) {
	o.Status.ObservedGeneration = gen
}
