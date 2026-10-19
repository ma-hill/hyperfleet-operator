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
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

// parseConfig round-trips a rendered document back into a generic map so tests
// assert on the serialized keys, not the Go structs.
func parseConfig(t *testing.T, out string) map[string]any {
	t.Helper()
	g := NewWithT(t)
	var m map[string]any
	g.Expect(yaml.Unmarshal([]byte(out), &m)).To(Succeed())
	return m
}

func TestRenderConfigIsDeterministic(t *testing.T) {
	g := NewWithT(t)

	in := configInput{
		ResourceType: testResourceType,
		APIBaseURL:   testAPIBaseURL,
		Namespace:    testNamespace,
	}

	first, err := renderConfig(in)
	g.Expect(err).NotTo(HaveOccurred())
	second, err := renderConfig(in)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).To(Equal(first))
	g.Expect(first).To(HavePrefix(configHeader))
}

func TestRenderConfigKeys(t *testing.T) {
	g := NewWithT(t)

	out, err := renderConfig(configInput{
		ResourceType: testResourceType,
		APIBaseURL:   testAPIBaseURL,
		Namespace:    testNamespace,
	})
	g.Expect(err).NotTo(HaveOccurred())

	cfg := parseConfig(t, out)

	sentinel := cfg["sentinel"].(map[string]any)
	g.Expect(sentinel["name"]).To(Equal(ResourceName(testResourceType)))

	g.Expect(cfg["debug_config"]).To(BeFalse())

	log := cfg["log"].(map[string]any)
	g.Expect(log["level"]).To(Equal(logLevel))
	g.Expect(log["format"]).To(Equal(logFormat))
	g.Expect(log["output"]).To(Equal(logOutput))

	clients := cfg["clients"].(map[string]any)
	api := clients["hyperfleet_api"].(map[string]any)
	g.Expect(api["base_url"]).To(Equal(testAPIBaseURL))
	g.Expect(api["version"]).To(Equal(apiClientVersion))
	g.Expect(api["timeout"]).To(Equal(apiClientTimeout))

	broker := clients["broker"].(map[string]any)
	g.Expect(broker["topic"]).To(Equal(brokerTopic(testNamespace, testResourceType)))

	g.Expect(cfg["resource_type"]).To(Equal(testResourceType))
	g.Expect(cfg["poll_interval"]).To(Equal(pollInterval))

	// Absent keys fall back to the binary's own defaults.
	g.Expect(cfg).NotTo(HaveKey("message_decision"))
	g.Expect(cfg).NotTo(HaveKey("message_data"))
}

func TestRenderBrokerConfigRabbitMQ(t *testing.T) {
	g := NewWithT(t)

	out, err := renderBrokerConfig(brokerConfigInput{BrokerType: BrokerTypeRabbitMQ})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(out).To(HavePrefix(brokerConfigHeader))

	cfg := parseConfig(t, out)
	broker := cfg["broker"].(map[string]any)
	g.Expect(broker["type"]).To(Equal(BrokerTypeRabbitMQ))
	rabbit := broker["rabbitmq"].(map[string]any)
	g.Expect(rabbit["exchange_type"]).To(Equal(brokerExchangeType))
	g.Expect(broker).NotTo(HaveKey("googlepubsub"))
}

func TestRenderBrokerConfigGooglePubSub(t *testing.T) {
	g := NewWithT(t)

	out, err := renderBrokerConfig(brokerConfigInput{
		BrokerType:      BrokerTypeGooglePubSub,
		BrokerProjectID: "my-gcp-project",
	})
	g.Expect(err).NotTo(HaveOccurred())

	cfg := parseConfig(t, out)
	broker := cfg["broker"].(map[string]any)
	g.Expect(broker["type"]).To(Equal(BrokerTypeGooglePubSub))
	pubsub := broker["googlepubsub"].(map[string]any)
	g.Expect(pubsub["project_id"]).To(Equal("my-gcp-project"))
	g.Expect(broker).NotTo(HaveKey("rabbitmq"))
}
