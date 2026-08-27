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

package api

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

// This file renders the API's config.yaml from the HyperFleetConfig spec
// (HYPERFLEET-1408). The rendered document is a strict subset of the API's
// configuration surface (github.com/openshift-hyperfleet/hyperfleet-api,
// pkg/config): only the keys the operator owns are emitted; everything else
// falls back to the API's own defaults. In particular the database block is
// deliberately absent — database credentials are injected as environment
// variables (secretKeyRef), never written into config.yaml.
//
// Determinism: sigs.k8s.io/yaml marshals via encoding/json and then sorts map
// keys, so a given input always produces byte-identical output. That stability
// is what makes the content-hash rollout (see the controller) meaningful and is
// asserted by the config golden/byte-stability tests.

// Filesystem layout inside the API container. The config file and any referenced
// secret material are mounted read-only under configMountPath; the paths written
// into config.yaml below must match the VolumeMounts declared in render.go.
const (
	// tlsMountPath is where the TLS Secret (kubernetes.io/tls) is mounted when
	// spec.api.tls is set; its keys surface as files under this directory.
	tlsMountPath = configMountPath + "/tls"
	// tlsCertFilePath / tlsKeyFilePath are the standard kubernetes.io/tls keys
	// (tls.crt, tls.key) as files, fed to the API's server.tls.{cert,key}_file.
	tlsCertFilePath = tlsMountPath + "/tls.crt"
	tlsKeyFilePath  = tlsMountPath + "/tls.key"
	tlsVolume       = "tls"

	// jwksMountPath is where the JWKS Secret is mounted when
	// spec.api.auth.jwkCertSecretRef is set.
	jwksMountPath = configMountPath + "/jwks"
	// jwksFilePath is the JWKS document as a file, fed to the API's
	// jwk_cert_file. The API parses this as a JSON Web Key Set
	// (keyfunc.NewJWKSetJSON), so the Secret key is "jwks.json".
	jwksFilePath = jwksMountPath + "/" + SecretKeyJWKS
	jwksVolume   = "jwks"

	// bindAllHost is the bind address for the server, health and metrics
	// listeners. It must be 0.0.0.0 (not localhost) so the kubelet can reach the
	// health/readiness endpoints and the metrics scraper can reach metrics from
	// outside the pod's loopback.
	bindAllHost = "0.0.0.0"
)

// Secret data keys the operator reads. These are a shared source of truth: the
// deployment builder (render.go) wires them into secretKeyRef/volume mounts and
// the controller reads the same keys for the rollout hash.
const (
	// Database credential keys (spec.api.database.secretRef). Note the API
	// consumes db.user as HYPERFLEET_DATABASE_USERNAME — the Secret key and the
	// env var name intentionally differ (matching the API Helm chart).
	SecretKeyDBHost     = "db.host"
	SecretKeyDBPort     = "db.port"
	SecretKeyDBName     = "db.name"
	SecretKeyDBUser     = "db.user"
	SecretKeyDBPassword = "db.password"

	// TLS keys (spec.api.tls.secretRef), a kubernetes.io/tls Secret.
	SecretKeyTLSCert = "tls.crt"
	SecretKeyTLSKey  = "tls.key"

	// JWKS key (spec.api.auth.jwkCertSecretRef); a JSON Web Key Set document.
	SecretKeyJWKS = "jwks.json"
)

// Database credential environment variable names, following the HyperFleet
// Configuration Standard (HYPERFLEET_<SECTION>_<KEY>). The API reads database
// credentials only from the environment (no *_FILE variant is wired for the
// non-file credential fields), so these are injected via secretKeyRef.
const (
	// envConfig points the API at the mounted config file (configFilePath). It is
	// a named constant alongside the database env vars so the single source of
	// truth is shared between render.go and its tests.
	envConfig = "HYPERFLEET_CONFIG"

	envDBHost     = "HYPERFLEET_DATABASE_HOST"
	envDBPort     = "HYPERFLEET_DATABASE_PORT"
	envDBName     = "HYPERFLEET_DATABASE_NAME"
	envDBUsername = "HYPERFLEET_DATABASE_USERNAME"
	envDBPassword = "HYPERFLEET_DATABASE_PASSWORD"
)

// configHeader is prepended to the marshaled YAML. It is a fixed string so it
// does not affect determinism, and it makes the ConfigMap self-describing for
// anyone who inspects it in-cluster.
const configHeader = `# HyperFleet API configuration — rendered by hyperfleet-operator from the
# HyperFleetConfig spec (HYPERFLEET-1408). Managed resource: manual edits are
# overwritten on the next reconcile.
`

// EntityDescriptor is the operator-side mirror of the API's
// registry.EntityDescriptor (github.com/openshift-hyperfleet/hyperfleet-api,
// pkg/registry). It is duplicated rather than imported to avoid taking a
// dependency on the API module for a handful of config keys; the json tags below
// must match the API's so the rendered YAML deserializes there. The entity set
// is bundle-specific and supplied by internal/bundle.
type EntityDescriptor struct {
	Kind              string   `json:"kind"`
	Plural            string   `json:"plural"`
	ParentKind        string   `json:"parent_kind,omitempty"`
	SpecSchemaName    string   `json:"spec_schema_name,omitempty"`
	OnParentDelete    string   `json:"on_parent_delete,omitempty"`
	RequiredAdapters  []string `json:"required_adapters,omitempty"`
	NameMinLen        int      `json:"name_min_len,omitempty"`
	NameMaxLen        int      `json:"name_max_len,omitempty"`
	RequireSpecSchema bool     `json:"require_spec_schema,omitempty"`
}

// configInput carries everything renderConfig needs, already resolved by the
// caller (Render). The JWKS source is pre-resolved to at most one of a URL or a
// file path: precedence and OIDC discovery are decided upstream, so this function
// stays a pure serializer.
type configInput struct {
	// AuthEnabled toggles server.jwt.enabled and whether an issuer config is
	// emitted at all.
	AuthEnabled bool
	// Issuer and Audience populate the single JWT issuer config when auth is on.
	Issuer   string
	Audience string
	// JWKCertURL / JWKCertFile are mutually exclusive; exactly one is non-empty
	// when auth is enabled. JWKCertURL is either the partner-pinned URL or the
	// OIDC-discovered jwks_uri; JWKCertFile is the in-pod path of a mounted JWKS
	// Secret.
	JWKCertURL  string
	JWKCertFile string
	// TLSEnabled emits the server.tls block pointing at the mounted cert/key.
	TLSEnabled bool
	// Entities is the bundle-specific entity registration list.
	Entities []EntityDescriptor
}

// apiConfig and its nested types are the operator-owned subset of the API's
// ApplicationConfig. Fields are pointers/omitempty where "absent" must mean
// "fall back to the API default" rather than "explicit zero value".
type apiConfig struct {
	Server   serverConfig       `json:"server"`
	Health   endpointConfig     `json:"health"`
	Metrics  endpointConfig     `json:"metrics"`
	Entities []EntityDescriptor `json:"entities,omitempty"`
}

type serverConfig struct {
	Host string     `json:"host"`
	Port int        `json:"port"`
	TLS  *tlsConfig `json:"tls,omitempty"`
	JWT  jwtConfig  `json:"jwt"`
}

type endpointConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type tlsConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}

type jwtConfig struct {
	Enabled bool `json:"enabled"`
	// Configs is omitempty so a disabled config emits `jwt: {enabled: false}`
	// with no empty list, matching what the API tolerates.
	Configs []jwtIssuerConfig `json:"configs,omitempty"`
}

// jwtIssuerConfig mirrors the API's JWTIssuerConfig for the fields the operator
// sets. header, identity_claim and identity_header are intentionally omitted so
// the API applies its own defaults (identity_claim defaults to "email",
// header to "Authorization"); the CR exposes no partner intent for them.
type jwtIssuerConfig struct {
	IssuerURL   string `json:"issuer_url"`
	Audience    string `json:"audience,omitempty"`
	JWKCertURL  string `json:"jwk_cert_url,omitempty"`
	JWKCertFile string `json:"jwk_cert_file,omitempty"`
}

// renderConfig serializes the operator-owned config subset to a deterministic
// YAML document (header + marshaled struct). It is pure: no cluster or network
// access, no time or randomness.
//
// It returns an error when auth is enabled but no JWKS source was resolved
// (neither URL nor file). That state should be unreachable — the controller
// resolves a URL (via the CR field or OIDC discovery) or a file (via the CR
// Secret ref) before calling Render — so the error exists to fail loudly on a
// wiring bug rather than emit a config the API would reject at startup.
func renderConfig(in configInput) (string, error) {
	cfg := apiConfig{
		Server: serverConfig{
			Host: bindAllHost,
			Port: portHTTP,
			JWT:  jwtConfig{Enabled: in.AuthEnabled},
		},
		Health:   endpointConfig{Host: bindAllHost, Port: portHealth},
		Metrics:  endpointConfig{Host: bindAllHost, Port: portMetrics},
		Entities: in.Entities,
	}

	if in.TLSEnabled {
		cfg.Server.TLS = &tlsConfig{
			Enabled:  true,
			CertFile: tlsCertFilePath,
			KeyFile:  tlsKeyFilePath,
		}
	}

	if in.AuthEnabled {
		if in.JWKCertURL == "" && in.JWKCertFile == "" {
			return "", fmt.Errorf("auth enabled but no JWKS source resolved (neither URL nor file)")
		}
		cfg.Server.JWT.Configs = []jwtIssuerConfig{{
			IssuerURL:   in.Issuer,
			Audience:    in.Audience,
			JWKCertURL:  in.JWKCertURL,
			JWKCertFile: in.JWKCertFile,
		}}
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal api config: %w", err)
	}
	return configHeader + string(out), nil
}
