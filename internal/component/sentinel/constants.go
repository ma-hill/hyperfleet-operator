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

const ComponentName = "sentinel"
const RelatedImageEnv = "RELATED_IMAGE_HYPERFLEET_SENTINEL"

// Broker constants and types
const BrokerSecretName = "hyperfleet-broker-credentials"
const BrokerSecretKey = "BROKER_RABBITMQ_URL"

type BrokerType string

const (
	BrokerTypeRabbitMQ     BrokerType = "rabbitmq"
	BrokerTypeGooglePubSub BrokerType = "googlepubsub"
)

// BrokerInfo carries the required variables set through environment variables
// and passed through the operator to the Sentinel component.
// ProjectID is required for Google Pub/Sub, and ignored for RabbitMQ.
// Type is required and must be one of the BrokerType* constants.
type BrokerInfo struct {
	Type      BrokerType
	ProjectID string
}

const (
	DefaultBrokerType              = BrokerTypeRabbitMQ
	BrokerTypeEnv                  = "BROKER_TYPE"
	BrokerGooglePubSubProjectIDEnv = "BROKER_GOOGLEPUBSUB_PROJECT_ID"
)

// ConfigMap keys
const (
	ConfigFileKey       = "config.yaml"
	BrokerConfigFileKey = "broker.yaml"
)

const (
	// Full path is /var/run/secrets/hyperfleet/api-token/token
	ServiceAccountTokenFileName = "token"
	ServiceAccountTokenPath     = "/var/run/secrets/hyperfleet/api-token"
)
