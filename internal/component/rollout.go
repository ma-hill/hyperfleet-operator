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

// Package component holds contract types shared between internal/bundle (which
// defines the Component interface and resolves bundles to component sets) and
// the component implementations under internal/component/<name>. It must stay a
// leaf: implementations import it to satisfy the interface's method signatures,
// while internal/bundle imports the implementations back to construct them, so
// the shared types cannot live in either place without an import cycle.
package component

// RolloutTargets names the operands that drive a component's config-rollout
// hash. The controller (see stampConfigHash in internal/controller) hashes the
// named ConfigMap's file content plus the referenced Secrets whose roles the
// component declares, and stamps the result on the named Deployment's pod
// template — so a config change or a Secret rotation rolls that Deployment and
// no other (HYPERFLEET-1673).
//
// Targets are static per component: they name well-known operands, not
// spec-dependent ones. The zero value means the component has no
// ConfigMap+Deployment rollout pair — nothing is stamped and no referenced
// Secret rolls its pods; the controller still folds the (content-only) hash
// into the applied-config metric.
type RolloutTargets struct {
	// ConfigMapName is the metadata.name of the component's configuration
	// ConfigMap within its rendered object set.
	ConfigMapName string
	// ConfigFileKey is the ConfigMap data key holding the rendered
	// configuration file that feeds the hash.
	ConfigFileKey string
	// DeploymentName is the metadata.name of the Deployment whose pod template
	// receives the config-hash annotation.
	DeploymentName string
	// SecretRoles lists the referenced-secret roles (the SecretRole* constants
	// below) this component consumes. Only hash entries with these ids are
	// folded into this component's hash, so a Secret rotated for another
	// component does not roll this one's pods. Empty means no referenced Secret
	// rolls this component.
	SecretRoles []string
}

// Referenced-secret roles: the join vocabulary between the controller's
// referenced-secret reader (which tags each Secret's hash entry with the role
// it plays for the CR spec) and each component's RolloutTargets.SecretRoles.
// Both sides must reference these constants rather than bare strings: a role
// the controller emits that no component declares would make that Secret's
// rotations silently stop rolling the pods that consume it.
const (
	// SecretRoleDatabase tags the database credential Secret
	// (spec.api.database.secretRef).
	SecretRoleDatabase = "database"
	// SecretRoleTLS tags the API serving-certificate Secret
	// (spec.api.tls.secretRef).
	SecretRoleTLS = "tls"
	// SecretRoleJWKS tags the JWKS document Secret
	// (spec.api.auth.jwkCertSecretRef).
	SecretRoleJWKS = "jwks"
)
