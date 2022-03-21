/*
Copyright 2021.

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

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

type DeletePolicyType string

const (
	DeletePolicyDelete DeletePolicyType = "delete"
	DeletePolicyRetain                  = "retain"
)

type SourceType string

const (
	SourceTypeSecret SourceType = "secret"
	SourceTypeVault             = "vault"
)

type DestType string

const (
	DestTypeNamespaces DestType = "namespaces"
	DestTypeVault               = "vault"
)

type SecretMirrorSource struct {
	// +kubebuilder:default:=secret
	// +kubebuilder:validation:Enum=secret;vault
	Type SourceType `json:"type,omitempty"`

	// +kubebuilder:validation:Required
	Name string `json:"name,omitempty"`
	// +optional
	Vault VaultSpec `json:"vault,omitempty"`
}

type SecretMirrorDestination struct {
	// +kubebuilder:default:=namespaces
	// +kubebuilder:validation:Enum=namespaces;vault
	Type       DestType `json:"type,omitempty"`
	Namespaces []string `json:"namespaces,omitempty"`
	// +optional
	Vault VaultSpec `json:"vault,omitempty"`
}

type MirrorStatus string

const (
	MirrorStatusPending MirrorStatus = "Pending"
	MirrorStatusActive               = "Active"
	MirrorStatusError                = "Error"
)

// SecretMirrorSpec defines the desired state of SecretMirror
type SecretMirrorSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:validation:Required
	Source      SecretMirrorSource      `json:"source,omitempty"`
	Destination SecretMirrorDestination `json:"destination,omitempty"`

	// +kubebuilder:validation:Enum=delete;retain
	DeletePolicy      DeletePolicyType `json:"deletePolicy,omitempty"`
	PollPeriodSeconds int64            `json:"pollPeriodSeconds,omitempty"`
}

// SecretMirrorStatus defines the observed state of SecretMirror
type SecretMirrorStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:default:=Pending
	// +kubebuilder:validation:Enum=Pending;Active;Error
	MirrorStatus MirrorStatus `json:"mirrorStatus,omitempty"`
	LastSyncTime metav1.Time  `json:"lastSyncTime,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion

// SecretMirror is the Schema for the secretmirrors API
// +kubebuilder:printcolumn:name="Source Type",type=string,JSONPath=`.spec.source.type`
// +kubebuilder:printcolumn:name="Source Name",type=string,JSONPath=`.spec.source.name`
// +kubebuilder:printcolumn:name="Destination Type",type=string,JSONPath=`.spec.destination.type`
// +kubebuilder:printcolumn:name="Delete Policy",type=string,JSONPath=`.spec.deletePolicy`
// +kubebuilder:printcolumn:name="Poll Period",type=integer,JSONPath=`.spec.pollPeriodSeconds`
// +kubebuilder:printcolumn:name="Mirror Status",type=string,JSONPath=`.status.mirrorStatus`
// +kubebuilder:printcolumn:name="Last Sync Time",type=string,JSONPath=`.status.lastSyncTime`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type SecretMirror struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SecretMirrorSpec   `json:"spec,omitempty"`
	Status SecretMirrorStatus `json:"status,omitempty"`
}

func (r *SecretMirror) PollPeriodDuration() time.Duration {
	return time.Duration(r.Spec.PollPeriodSeconds) * time.Second
}

//+kubebuilder:object:root=true

// SecretMirrorList contains a list of SecretMirror
type SecretMirrorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SecretMirror `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SecretMirror{}, &SecretMirrorList{})
}
