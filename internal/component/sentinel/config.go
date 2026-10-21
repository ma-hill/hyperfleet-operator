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
	BaseURL string `json:"base_url"`
	Version string `json:"version"`
	Timeout string `json:"timeout"`
}

type brokerClientConfig struct {
	Topic string `json:"topic"`
}

// message_decision and message_data are the CEL defaults the
// hyperfleet-sentinel chart ships; the binary validates both as required.
type messageDecisionConfig struct {
	Params []messageDecisionParam `json:"params"`
	Result string                 `json:"result"`
}

type messageDecisionParam struct {
	Name string `json:"name"`
	Expr string `json:"expr"`
}

type messageDataConfig struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Href       string `json:"href"`
	Generation string `json:"generation"`
}

func defaultMessageDecision() messageDecisionConfig {
	return messageDecisionConfig{
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
	}
}

func defaultMessageData() messageDataConfig {
	return messageDataConfig{
		ID:         "resource.id",
		Kind:       "resource.kind",
		Href:       "resource.href",
		Generation: "resource.generation",
	}
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

// renderConfig serializes config.yaml for one instance.
func renderConfig(in configInput) (string, error) {
	cfg := sentinelConfig{
		Sentinel: sentinelInfo{Name: ResourceName(in.ResourceType)},
		Log: logConfig{
			Level:  logLevel,
			Format: logFormat,
			Output: logOutput,
		},
		Clients: clientsConfig{
			HyperFleetAPI: hyperfleetAPIClientConfig{
				BaseURL: in.APIBaseURL,
				Version: apiClientVersion,
				Timeout: apiClientTimeout,
			},
			Broker: brokerClientConfig{Topic: in.BrokerTopic},
		},
		ResourceType:    in.ResourceType,
		PollInterval:    pollInterval,
		MessageDecision: defaultMessageDecision(),
		MessageData:     defaultMessageData(),
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal sentinel config: %w", err)
	}
	return configHeader + string(out), nil
}

// brokerTopic namespaces the topic by the operator's namespace so resource
// types never collide on the same topic.
func brokerTopic(namespace, resourceType string) string {
	return namespace + "-" + resourceType
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
