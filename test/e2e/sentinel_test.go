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

	"github.com/openshift-hyperfleet/hyperfleet-operator/internal/bundle"
	"sigs.k8s.io/yaml"
)

var sentinelResourceTypes = func() []string {
	types := make([]string, 0,
		len(bundle.CloudCAPIEntities))
	for _, e := range bundle.CloudCAPIEntities {
		types = append(types, e.Plural)
	}
	return types
}()

const (
	// sentinelConfigJSONPath extracts the config.yaml key from a Sentinel
	// ConfigMap; jsonpath requires the dot in the key to be escaped or it is
	// read as nested fields.
	sentinelConfigJSONPath = `jsonpath={.data.config\.yaml}`
	// sentinelBrokerConfigJSONPath extracts the broker.yaml key the same way.
	sentinelBrokerConfigJSONPath = `jsonpath={.data.broker\.yaml}`
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

// sentinelE2EBrokerConfig is the subset of the rendered broker.yaml the e2e
// suite asserts on; the json tags match the sentinel component's broker.yaml
// contract. The empty rabbitmq url key is load-bearing: registering it lets
// the BROKER_RABBITMQ_URL env var from the broker Secret supply the runtime
// value, so credentials must never appear in the ConfigMap.
type sentinelE2EBrokerConfig struct {
	Broker struct {
		Type     string `json:"type"`
		RabbitMQ *struct {
			URL          string `json:"url"`
			ExchangeType string `json:"exchange_type"`
		} `json:"rabbitmq"`
	} `json:"broker"`
}

// parseSentinelBrokerConfig unmarshals one Sentinel's rendered broker.yaml,
// including the operator's comment header, which is valid YAML.
func parseSentinelBrokerConfig(brokerYAML string) (sentinelE2EBrokerConfig, error) {
	var cfg sentinelE2EBrokerConfig
	if err := yaml.Unmarshal([]byte(brokerYAML), &cfg); err != nil {
		return cfg, fmt.Errorf("parse sentinel broker.yaml: %w", err)
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
