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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KubeStateMetricsConfigSpec defines the desired state of KubeStateMetricsConfig
type KubeStateMetricsConfigSpec struct {
	// TargetNamespace is the namespace where the ConfigMap will be created
	// This should match the namespace where kube-state-metrics is deployed
	// If not specified, defaults to "observability"
	// +optional
	// +kubebuilder:default:="observability"
	TargetNamespace string `json:"targetNamespace,omitempty"`

	// CustomResourceStateConfig contains the custom resource state metrics configuration
	// This is the content that will be written to custom-resource-state-config.yaml
	// +optional
	CustomResourceStateConfig string `json:"customResourceStateConfig,omitempty"`

	// Config contains the standard kube-state-metrics configuration
	// This is the content that will be written to config.yaml for non-custom resources
	// +optional
	Config string `json:"config,omitempty"`

	// AdditionalConfigs allows for additional configuration files to be created
	// Key is the filename, value is the file content
	// +optional
	AdditionalConfigs map[string]string `json:"additionalConfigs,omitempty"`
}

// KubeStateMetricsConfigStatus defines the observed state of KubeStateMetricsConfig
type KubeStateMetricsConfigStatus struct {
	// conditions represent the current state of the KubeStateMetricsConfig resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation of this resource that was last reconciled by the controller.
	ObservedGeneration int64 `json:"observedGeneration"`

	// Phase is the current phase of the resource.
	Phase string `json:"phase"`

	// ConfigMapName is the name of the ConfigMap created for this configuration
	// +optional
	ConfigMapName string `json:"configMapName,omitempty"`

	// ConfigMapNamespace is the namespace of the ConfigMap created for this configuration
	// +optional
	ConfigMapNamespace string `json:"configMapNamespace,omitempty"`
}

// KubeStateMetricsConfig is the Schema for the kubestatemetricsconfigs API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.configMapName`,name="ConfigMap",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.phase`,name="Phase",type=string
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=onboarding"
type KubeStateMetricsConfig struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of KubeStateMetricsConfig
	// +required
	Spec KubeStateMetricsConfigSpec `json:"spec"`

	// status defines the observed state of KubeStateMetricsConfig
	// +optional
	Status KubeStateMetricsConfigStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// KubeStateMetricsConfigList contains a list of KubeStateMetricsConfig
type KubeStateMetricsConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeStateMetricsConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeStateMetricsConfig{}, &KubeStateMetricsConfigList{})
}

// Finalizer returns the finalizer string for the KubeStateMetricsConfig resource
func (o *KubeStateMetricsConfig) Finalizer() string {
	return GroupVersion.Group + "/config-finalizer"
}

// GetStatus returns the status of the KubeStateMetricsConfig resource
func (o *KubeStateMetricsConfig) GetStatus() any {
	return o.Status
}

// GetConditions returns the conditions of the KubeStateMetricsConfig resource
func (o *KubeStateMetricsConfig) GetConditions() *[]metav1.Condition {
	return &o.Status.Conditions
}

// SetPhase sets the phase of the KubeStateMetricsConfig resource status
func (o *KubeStateMetricsConfig) SetPhase(phase string) {
	o.Status.Phase = phase
}

// SetObservedGeneration sets the observed generation of the KubeStateMetricsConfig resource
func (o *KubeStateMetricsConfig) SetObservedGeneration(gen int64) {
	o.Status.ObservedGeneration = gen
}
