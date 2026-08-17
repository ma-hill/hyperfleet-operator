/*
Copyright 2026.

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

// NOTE: json tags are required. Any new fields you add must have json tags for
// the fields to be serialized. Run "make generate manifests" after editing.

// SingletonName is the only permitted name for a HyperFleetConfig. The resource
// is a cluster-scoped singleton: a CEL validation rule pins the name to
// "cluster", and cluster-scoped name uniqueness then guarantees at most one
// instance (a second create is rejected by the API server as AlreadyExists, not
// by CEL, since CEL cannot see other objects). No admission webhooks are used
// (see architecture ADR-0019).
const SingletonName = "cluster"

// BundleType selects one of the operator-internal bundle definitions. It is a
// selector only: the CR carries the choice of a bundle, never its contents. The
// bundle controller (HYPERFLEET-1407) resolves the selected bundle into a
// concrete component set.
//
// These constants are the single source of truth for the valid bundle values
// and MUST stay in lockstep with the bundle definitions shipped inside the
// operator. Adding a bundle means adding a constant here and the matching enum
// value in the +kubebuilder:validation:Enum marker below.
//
// +kubebuilder:validation:Enum=cloud-capi;onprem-agent
type BundleType string

const (
	// BundleCloudCAPI is the cloud, CAPI-based provisioning deployment
	// (managed OpenShift on a public cloud).
	BundleCloudCAPI BundleType = "cloud-capi"
	// BundleOnPremAgent is the on-premise, air-gapped (agent-based) deployment.
	BundleOnPremAgent BundleType = "onprem-agent"
)

// AllBundleTypes is the Go-level source of truth for the valid BundleType
// values. The +kubebuilder:validation:Enum marker on BundleType must list
// exactly these values; the lockstep guard test creates a HyperFleetConfig with
// each entry and fails if any is not accepted by the CRD, catching drift between
// the constants and the enum marker.
var AllBundleTypes = []BundleType{BundleCloudCAPI, BundleOnPremAgent}

// SizingProfile expresses sizing intent, not replica engineering. The operator
// maps each profile to concrete replicas, resource requests/limits and HPA/PDB
// defaults for the operand.
//
// +kubebuilder:validation:Enum=small;medium;large
type SizingProfile string

const (
	// SizingProfileSmall is the default, lowest-footprint sizing profile.
	SizingProfileSmall SizingProfile = "small"
	// SizingProfileMedium is a mid-range sizing profile.
	SizingProfileMedium SizingProfile = "medium"
	// SizingProfileLarge is the highest-footprint sizing profile.
	SizingProfileLarge SizingProfile = "large"
)

// AllSizingProfiles is the Go-level source of truth for the valid SizingProfile
// values; it must match the +kubebuilder:validation:Enum marker on
// SizingProfile (asserted by the lockstep guard test).
var AllSizingProfiles = []SizingProfile{SizingProfileSmall, SizingProfileMedium, SizingProfileLarge}

// Condition types reported on HyperFleetConfig status. This is deliberate
// operator-layer vocabulary describing installation health, and is distinct from
// the HyperFleet API's own resource-condition vocabulary (Available/Ready/
// Reconciled/LastKnownReconciled/per-adapter; see architecture ADR-0007 and
// ADR-0008). The bundle controller (HYPERFLEET-1409) populates these; this story
// defines the schema only.
const (
	// ConditionAvailable is True when the installed operand (the API) is
	// deployed and healthy.
	ConditionAvailable = "Available"
	// ConditionProgressing is True while the operator is actively rolling out a
	// change to the operand.
	ConditionProgressing = "Progressing"
	// ConditionDegraded is True when the operator cannot reach or maintain the
	// desired state.
	ConditionDegraded = "Degraded"
)

// SecretReference references a Secret by name. Referenced Secrets must live in
// the operator's own namespace: because HyperFleetConfig is cluster-scoped, no
// namespace field is exposed (name-only + operator-namespace convention, decided
// in the HYPERFLEET-1406 API review).
type SecretReference struct {
	// name is the name of the Secret in the operator's namespace. It must be a
	// valid DNS-1123 subdomain (the same constraint the API server places on
	// Secret names) so an unresolvable reference is rejected at admission rather
	// than failing opaquely when the reference is later resolved.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`
}

// DatabaseSpec configures the HyperFleet API's connection to its external
// PostgreSQL database. The database is partner-provided; the operator never
// provisions it.
type DatabaseSpec struct {
	// secretRef references a Secret holding the database connection credentials.
	// The Secret must provide the keys db.host, db.port, db.name, db.user and
	// db.password.
	//
	// +kubebuilder:validation:Required
	SecretRef SecretReference `json:"secretRef"`
}

// AuthSpec configures partner-facing JWT authentication intent for the API.
// Machinery details (JWKS rotation, public-path allowlist) remain
// operator-internal defaults and are not exposed here.
//
// +kubebuilder:validation:XValidation:rule="!self.enabled || (has(self.issuer) && has(self.audience))",message="issuer and audience are required when auth is enabled"
type AuthSpec struct {
	// enabled turns JWT authentication on for the API endpoint. It defaults to
	// true, so a config that omits it gets authentication ON. It is a pointer to
	// distinguish "unset" (apply the default, true) from an explicit false
	// (disable auth), which a non-pointer bool cannot express: with omitempty a
	// plain false is dropped and re-defaulted to true, so auth could never be
	// turned off via the typed client; without omitempty an unset field serializes
	// as false and suppresses the default. Only *bool avoids both traps.
	//
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// issuer is the OIDC issuer URL that mints accepted tokens. Required when
	// enabled is true. Whenever it is set (regardless of enabled) it must be a
	// valid https URL with a host, so a malformed issuer is rejected at admission
	// rather than surfacing later at token-validation time.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() == 'https' && url(self).getHostname() != ''",message="issuer must be a valid https URL"
	// +optional
	Issuer string `json:"issuer,omitempty"`

	// audience is the token audience the API requires. Required and non-empty
	// when enabled is true.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	Audience string `json:"audience,omitempty"`
}

// TLSSpec configures TLS for the API endpoint. The certificate material is
// referenced, not described.
type TLSSpec struct {
	// secretRef references a kubernetes.io/tls Secret (providing tls.crt and
	// tls.key) used to serve the API endpoint.
	//
	// +kubebuilder:validation:Required
	SecretRef SecretReference `json:"secretRef"`
}

// APISpec is the partner-facing configuration for the HyperFleet API component,
// which lives in the shared tier of every bundle.
type APISpec struct {
	// database configures the external PostgreSQL connection.
	//
	// +kubebuilder:validation:Required
	Database DatabaseSpec `json:"database"`

	// auth configures partner-facing JWT authentication intent.
	//
	// +kubebuilder:validation:Required
	Auth AuthSpec `json:"auth"`

	// tls optionally configures TLS for the API endpoint. When omitted, the
	// operator applies its default serving configuration.
	//
	// +optional
	TLS *TLSSpec `json:"tls,omitempty"`

	// profile selects a sizing profile for the API. Defaults to "small".
	//
	// +kubebuilder:default=small
	// +optional
	Profile SizingProfile `json:"profile,omitempty"`
}

// HyperFleetConfigSpec defines the desired state of HyperFleetConfig. It captures
// partner intent only; internal machinery (broker, adapters, Sentinel) is never
// expressed here.
type HyperFleetConfigSpec struct {
	// bundle selects one of the operator-internal bundle definitions. It is
	// immutable after creation: switching deployments requires recreating the
	// resource.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="bundle is immutable"
	Bundle BundleType `json:"bundle"`

	// api is the partner-facing configuration for the HyperFleet API component.
	//
	// +kubebuilder:validation:Required
	API APISpec `json:"api"`
}

// HyperFleetConfigStatus defines the observed state of HyperFleetConfig. It is
// populated by the bundle controller in later stories; this story defines the
// schema only.
type HyperFleetConfigStatus struct {
	// observedGeneration is the .metadata.generation the operator last acted on.
	//
	// +kubebuilder:validation:Minimum=0
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current installation health of the operand.
	// Recognized types are Available, Progressing and Degraded.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=hfc
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="the only permitted name is 'cluster'; HyperFleetConfig is a cluster-scoped singleton"
// +kubebuilder:printcolumn:name="Bundle",type=string,JSONPath=`.spec.bundle`
// +kubebuilder:printcolumn:name="Profile",type=string,JSONPath=`.spec.api.profile`
// +kubebuilder:printcolumn:name="Available",type=string,JSONPath=`.status.conditions[?(@.type=="Available")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// HyperFleetConfig is the Schema for the hyperfleetconfigs API. It is a
// cluster-scoped singleton: exactly one instance, named "cluster", is permitted.
type HyperFleetConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec   HyperFleetConfigSpec   `json:"spec"`
	Status HyperFleetConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HyperFleetConfigList contains a list of HyperFleetConfig.
type HyperFleetConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HyperFleetConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HyperFleetConfig{}, &HyperFleetConfigList{})
}
