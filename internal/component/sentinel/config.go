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

package sentinel

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

const (
	pollInterval       = "5s"
	logLevel           = "info"
	logFormat          = "json"
	logOutput          = "stdout"
	apiClientVersion   = "v1"
	apiClientTimeout   = "10s"
	brokerExchangeType = "topic"
)

const configHeader = `# Sentinel configuration — rendered by hyperfleet-operator (HYPERFLEET-1673).
# Managed resource: manual edits are overwritten on the next reconcile.
`

const brokerConfigHeader = `# Broker configuration for the hyperfleet-broker library — rendered by
# hyperfleet-operator (HYPERFLEET-1673). Managed resource: manual edits are
# overwritten on the next reconcile.
`

type configInput struct {
	ResourceType string
	APIBaseURL   string
	BrokerTopic  string
	HasParent    bool
	AuthEnabled  bool
}

// sentinelConfig and its nested types are the operator-owned subset of
// Sentinel's config.yaml; the json tags must match the keys
// hyperfleet-sentinel's pkg/config deserializes.
type sentinelConfig struct {
	Sentinel        sentinelInfo          `json:"sentinel"`
	DebugConfig     bool                  `json:"debug_config"`
	Log             logConfig             `json:"log"`
	Clients         clientsConfig         `json:"clients"`
	ResourceType    string                `json:"resource_type"`
	PollInterval    string                `json:"poll_interval"`
	MessageDecision messageDecisionConfig `json:"message_decision"`
	MessageData     messageDataConfig     `json:"message_data"`
	TracingEnabled  bool                  `json:"tracing_enabled"`
}

type sentinelInfo struct {
	Name string `json:"name"`
}

type logConfig struct {
	Level  string `json:"level"`
	Format string `json:"format"`
	Output string `json:"output"`
}

type clientsConfig struct {
	HyperFleetAPI hyperfleetAPIClientConfig `json:"hyperfleet_api"`
	Broker        brokerClientConfig        `json:"broker"`
}

type hyperfleetAPIClientConfig struct {
	BaseURL string      `json:"base_url"`
	Version string      `json:"version"`
	Timeout string      `json:"timeout"`
	Auth    *authConfig `json:"auth,omitempty"`
}

type authConfig struct {
	TokenPath     string `json:"token_path"`
	Scheme        string `json:"scheme"`
	TokenCacheTTL string `json:"token_cache_ttl,omitempty"`
}

type brokerClientConfig struct {
	Topic string `json:"topic"`
}

// broker.yaml nests everything under one top-level broker key; exactly one of
// the type-specific blocks is set per render.
type brokerFileDocument struct {
	Broker brokerFileConfig `json:"broker"`
}

type brokerFileConfig struct {
	Type         BrokerType          `json:"type"`
	RabbitMQ     *rabbitMQConfig     `json:"rabbitmq,omitempty"`
	GooglePubSub *googlePubSubConfig `json:"googlepubsub,omitempty"`
}

type rabbitMQConfig struct {
	URL          string `json:"url"`
	ExchangeType string `json:"exchange_type"`
}

type googlePubSubConfig struct {
	ProjectID            string `json:"project_id"`
	CreateTopicIfMissing bool   `json:"create_topic_if_missing"`
}

type messageDecisionConfig struct {
	Params []messageDecisionParam `json:"params"`
	Result string                 `json:"result"`
}

type messageDecisionParam struct {
	Name string `json:"name"`
	Expr string `json:"expr"`
}

type ownerReferenceConfig struct {
	ID   string `json:"id"`
	Href string `json:"href"`
	Kind string `json:"kind"`
}

type messageDataConfig struct {
	ID              string                `json:"id"`
	Kind            string                `json:"kind"`
	Href            string                `json:"href"`
	Generation      string                `json:"generation"`
	OwnerReferences *ownerReferenceConfig `json:"owner_references,omitempty"`
}

// renderConfig serializes config.yaml for one instance.
func renderConfig(in configInput) (string, error) {
	APIClientConfig := &hyperfleetAPIClientConfig{
		BaseURL: in.APIBaseURL,
		Version: apiClientVersion,
		Timeout: apiClientTimeout,
	}
	// If auth is enabled, append the auth values into the config
	if in.AuthEnabled {
		APIClientConfig.Auth = &authConfig{
			TokenPath:     fmt.Sprintf("%s/%s", ServiceAccountTokenPath, ServiceAccountTokenFileName),
			Scheme:        "Bearer",
			TokenCacheTTL: "30s",
		}
	}

	msgDecision, err := constructMessageDecision(in.ResourceType)
	if err != nil {
		return "", fmt.Errorf("construct message decision: %w", err)
	}
	msgData := constructMessageData(in.HasParent)

	cfg := sentinelConfig{
		Sentinel: sentinelInfo{Name: ResourceName(in.ResourceType)},
		Log: logConfig{
			Level:  logLevel,
			Format: logFormat,
			Output: logOutput,
		},
		Clients: clientsConfig{
			HyperFleetAPI: *APIClientConfig,
			Broker:        brokerClientConfig{Topic: in.BrokerTopic},
		},
		ResourceType:    in.ResourceType,
		PollInterval:    pollInterval,
		MessageDecision: *msgDecision,
		MessageData:     *msgData,
		TracingEnabled:  false,
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal sentinel config: %w", err)
	}
	return configHeader + string(out), nil
}

// renderBrokerConfig serializes broker.yaml. It errors on a broker type that
// is not one of the BrokerType* constants, or on googlepubsub without a
// project id, rather than emitting a config Sentinel would reject at startup.
func renderBrokerConfig(brokerInfo *BrokerInfo) (string, error) {
	cfg := brokerFileConfig{}
	switch brokerInfo.Type {
	case BrokerTypeGooglePubSub:
		if brokerInfo.ProjectID == "" {
			return "", fmt.Errorf("%s is required when %s is %q", BrokerGooglePubSubProjectIDEnv, BrokerTypeEnv, BrokerTypeGooglePubSub)
		}
		cfg.Type = BrokerTypeGooglePubSub
		cfg.GooglePubSub = &googlePubSubConfig{
			ProjectID:            brokerInfo.ProjectID,
			CreateTopicIfMissing: false,
		}
	case BrokerTypeRabbitMQ:
		cfg.Type = BrokerTypeRabbitMQ
		cfg.RabbitMQ = &rabbitMQConfig{
			URL:          "amqp://placeholder:placeholder@rabbitmq.placeholder.svc.cluster.local:5672/",
			ExchangeType: brokerExchangeType,
		}
	default:
		return "", fmt.Errorf("unknown %s %q: must be %q or %q", BrokerTypeEnv, brokerInfo.Type, BrokerTypeRabbitMQ, BrokerTypeGooglePubSub)
	}

	out, err := yaml.Marshal(brokerFileDocument{Broker: cfg})
	if err != nil {
		return "", fmt.Errorf("marshal sentinel broker config: %w", err)
	}
	return brokerConfigHeader + string(out), nil
}

func constructMessageData(hasParent bool) (md *messageDataConfig) {
	md = &messageDataConfig{
		ID:         "resource.id",
		Kind:       "resource.kind",
		Href:       "resource.href",
		Generation: "resource.generation",
	}
	// child resources need to include owner references
	if hasParent {
		md.OwnerReferences = &ownerReferenceConfig{
			ID:   "resource.owner_references.id",
			Href: "resource.owner_references.href",
			Kind: "resource.owner_references.kind",
		}
	}
	return
}

// constructMessageDecision constructs the message_decision config for the given resource type.
// It returns an error if the resource type is not supported.
func constructMessageDecision(resourceType string) (*messageDecisionConfig, error) {
	switch resourceType {
	case "nodepools", "clusters":
		return &messageDecisionConfig{
			Params: []messageDecisionParam{
				{Name: "ref_time", Expr: `condition("Reconciled").last_updated_time`},
				{Name: "is_reconciled", Expr: `condition("Reconciled").status == "True"`},
				{Name: "has_ref_time", Expr: `ref_time != ""`},
				{Name: "is_new_resource", Expr: "resource.generation == 1 && !has_ref_time"},
				{Name: "generation_mismatch", Expr: `resource.generation > condition("Reconciled").observed_generation`},
				{Name: "reconciled_and_stale", Expr: `is_reconciled && has_ref_time && now - timestamp(ref_time) > duration("30m")`},
				{Name: "not_reconciled_and_debounced", Expr: `!is_reconciled && has_ref_time && now - timestamp(ref_time) > duration("10s")`},
			},
			Result: "is_new_resource || generation_mismatch || reconciled_and_stale || not_reconciled_and_debounced",
		}, nil
	// For versions, wifconfigs, and channels, Sentinel publishes only when it evaluates the resource within five minutes of updated_time.
	// If Sentinel cannot evaluate an update before that window expires, the update can go unpublished.
	// This rule does not guarantee recovery of missed updates.
	case "versions", "wifconfigs", "channels":
		return &messageDecisionConfig{
			Params: []messageDecisionParam{
				{Name: "recently_updated", Expr: `now - timestamp(resource.updated_time) < duration("5m")`},
			},
			Result: "recently_updated",
		}, nil
	}
	return nil, fmt.Errorf("no message_decision defined for resource type %q", resourceType)
}

// brokerTopic namespaces the topic by the operator's namespace so resource
// types never collide on the same topic.
func brokerTopic(namespace, resourceType string) string {
	return namespace + "-" + resourceType
}
