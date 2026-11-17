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

package e2e

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

// cloudCAPIResourceTypes lists the plural resource types the cloud-capi
// bundle registers (internal/bundle.CloudCAPIEntities); one Sentinel instance
// is rendered per entry. Keep in sync when the entity set changes.
var cloudCAPIResourceTypes = []string{"clusters", "nodepools", "channels", "versions", "wifconfigs"}

const (
	// sentinelConfigJSONPath extracts the config.yaml key from a Sentinel
	// ConfigMap; jsonpath requires the dot in the key to be escaped or it is
	// read as nested fields.
	sentinelConfigJSONPath = `jsonpath={.data.config\.yaml}`
	// sentinelTokenPath must match the projected token path the sentinel
	// component renders when API auth is enabled.
	sentinelTokenPath = "/var/run/secrets/hyperfleet/api-token/token"
)

// kubectl resource kinds used by the operand assertions.
const (
	kindDeployment     = "deployment"
	kindServiceAccount = "serviceaccount"
	kindConfigMap      = "configmap"
	kindNetworkPolicy  = "networkpolicy"
)

// sentinelOperands holds the operand resource names for one resource type,
// mirroring the sentinel component's naming. No Service is rendered: Sentinel
// is an outbound poller with no in-cluster caller.
type sentinelOperands struct {
	Deployment      string
	ServiceAccount  string
	ConfigMap       string
	BrokerConfigMap string
	NetworkPolicy   string
}

// sentinelOperandNames returns the operand names for one resource type.
func sentinelOperandNames(resourceType string) sentinelOperands {
	name := "hyperfleet-sentinel-" + resourceType
	return sentinelOperands{
		Deployment:      name,
		ServiceAccount:  name,
		ConfigMap:       name + "-config",
		BrokerConfigMap: name + "-broker-config",
		NetworkPolicy:   name,
	}
}

// sentinelE2EConfig is the subset of the rendered config.yaml the e2e suite
// asserts on; the json tags match the operator-owned contract with
// hyperfleet-sentinel's pkg/config.
type sentinelE2EConfig struct {
	Clients struct {
		HyperFleetAPI struct {
			BaseURL string `json:"base_url"`
			Auth    *struct {
				TokenPath string `json:"token_path"`
				Scheme    string `json:"scheme"`
			} `json:"auth"`
		} `json:"hyperfleet_api"`
		Broker struct {
			Topic string `json:"topic"`
		} `json:"broker"`
	} `json:"clients"`
	ResourceType string `json:"resource_type"`
}

// parseSentinelConfig unmarshals one Sentinel's rendered config.yaml,
// including the operator's comment header, which is valid YAML.
func parseSentinelConfig(configYAML string) (sentinelE2EConfig, error) {
	var cfg sentinelE2EConfig
	if err := yaml.Unmarshal([]byte(configYAML), &cfg); err != nil {
		return cfg, fmt.Errorf("parse sentinel config.yaml: %w", err)
	}
	return cfg, nil
}

// sentinelPodLabel matches the pods of one Sentinel instance; the label value
// is the component name (sentinel-<resource type>), not the resource name.
func sentinelPodLabel(resourceType string) string {
	return "app.kubernetes.io/name=sentinel-" + resourceType
}

// sentinelReadyJSONPath extracts a Sentinel pod's Ready condition status.
// Sentinel's /readyz gates on broker health and a successful poll, so Ready
// is the contract-backed signal that the broker connection works.
const sentinelReadyJSONPath = "jsonpath={.items[0].status.conditions[?(@.type=='Ready')].status}"

// fixtureDummyIssuer is the placeholder issuer in
// test/e2e/hyperfleetconfig.yaml, replaced with the test cluster's real OIDC
// issuer before the manifest is applied.
const fixtureDummyIssuer = "https://issuer.example.com"

// parseOIDCIssuer extracts the issuer URL from an API server
// /.well-known/openid-configuration document.
func parseOIDCIssuer(discoveryJSON string) (string, error) {
	var doc struct {
		Issuer string `json:"issuer"`
	}
	if err := yaml.Unmarshal([]byte(discoveryJSON), &doc); err != nil {
		return "", fmt.Errorf("parse openid-configuration: %w", err)
	}
	if doc.Issuer == "" {
		return "", fmt.Errorf("openid-configuration has no issuer")
	}
	return doc.Issuer, nil
}
