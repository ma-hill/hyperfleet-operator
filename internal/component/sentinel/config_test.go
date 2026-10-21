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
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

func TestRenderConfigDefaults(t *testing.T) {
	g := NewWithT(t)

	out, err := renderConfig(configInput{
		ResourceType: testResourceType,
		APIBaseURL:   testAPIBaseURL,
		BrokerTopic:  testBrokerTopic,
	})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(out).To(HavePrefix("# Sentinel configuration"))
	g.Expect(out).To(ContainSubstring("resource_type: " + testResourceType))
	g.Expect(out).To(ContainSubstring("base_url: " + testAPIBaseURL))
	g.Expect(out).To(ContainSubstring("topic: " + testBrokerTopic))
	g.Expect(out).To(ContainSubstring("name: " + ResourceName(testResourceType)))
	g.Expect(out).To(ContainSubstring("poll_interval: 5s"))
	g.Expect(out).To(ContainSubstring("level: info"))
	g.Expect(out).To(ContainSubstring("format: json"))
	g.Expect(out).To(ContainSubstring("version: v1"))
	g.Expect(out).To(ContainSubstring("timeout: 10s"))
	g.Expect(out).To(ContainSubstring("is_new_resource"))
	g.Expect(out).To(ContainSubstring("generation_mismatch"))
	g.Expect(out).To(ContainSubstring("reconciled_and_stale"))
}

func TestRenderConfigIsDeterministic(t *testing.T) {
	g := NewWithT(t)

	in := configInput{
		ResourceType: testResourceType,
		APIBaseURL:   testAPIBaseURL,
		BrokerTopic:  testBrokerTopic,
	}
	out1, err := renderConfig(in)
	g.Expect(err).NotTo(HaveOccurred())
	out2, err := renderConfig(in)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(out1).To(Equal(out2))
}

func TestRenderDefaultConfigIsValidYAML(t *testing.T) {
	g := NewWithT(t)

	out, err := renderConfig(configInput{
		ResourceType: testResourceType,
		APIBaseURL:   testAPIBaseURL,
		BrokerTopic:  testBrokerTopic,
	})
	g.Expect(err).NotTo(HaveOccurred())

	// Strip the comment header before unmarshaling.
	body := out[strings.Index(out, "\n")+1:]
	body = body[strings.Index(body, "\n")+1:]
	var parsed sentinelConfig
	g.Expect(yaml.Unmarshal([]byte(body), &parsed)).To(Succeed())

	g.Expect(parsed.Sentinel.Name).To(Equal(ResourceName(testResourceType)))
	g.Expect(parsed.ResourceType).To(Equal(testResourceType))
	g.Expect(parsed.PollInterval).To(Equal("5s"))

	g.Expect(parsed.Log.Level).To(Equal("info"))
	g.Expect(parsed.Log.Format).To(Equal("json"))
	g.Expect(parsed.Log.Output).To(Equal("stdout"))

	g.Expect(parsed.Clients.HyperFleetAPI.BaseURL).To(Equal(testAPIBaseURL))
	g.Expect(parsed.Clients.HyperFleetAPI.Version).To(Equal("v1"))
	g.Expect(parsed.Clients.HyperFleetAPI.Timeout).To(Equal("10s"))
	g.Expect(parsed.Clients.Broker.Topic).To(Equal(testBrokerTopic))

	g.Expect(parsed.MessageDecision.Params).NotTo(BeEmpty())
	g.Expect(parsed.MessageDecision.Result).To(ContainSubstring("is_new_resource"))
	g.Expect(parsed.MessageData.ID).To(Equal("resource.id"))
	g.Expect(parsed.MessageData.Kind).To(Equal("resource.kind"))
	g.Expect(parsed.MessageData.Href).To(Equal("resource.href"))
	g.Expect(parsed.MessageData.Generation).To(Equal("resource.generation"))
}

// --- renderBrokerConfig tests ---

func TestRenderBrokerConfigRabbitMQ(t *testing.T) {
	g := NewWithT(t)

	out, err := renderBrokerConfig(&BrokerInfo{Type: BrokerTypeRabbitMQ})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(out).To(HavePrefix("# Broker configuration"))
	g.Expect(out).To(ContainSubstring("type: rabbitmq"))
	g.Expect(out).To(ContainSubstring("exchange_type: topic"))
	g.Expect(out).NotTo(ContainSubstring("googlepubsub"))
}

func TestRenderBrokerConfigGooglePubSub(t *testing.T) {
	g := NewWithT(t)

	out, err := renderBrokerConfig(&BrokerInfo{
		Type:      BrokerTypeGooglePubSub,
		ProjectID: "my-gcp-project",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(out).To(ContainSubstring("type: googlepubsub"))
	g.Expect(out).To(ContainSubstring("project_id: my-gcp-project"))
	g.Expect(out).To(ContainSubstring("create_topic_if_missing: false"))
	g.Expect(out).NotTo(ContainSubstring("rabbitmq"))
}

func TestRenderBrokerConfigGooglePubSubMissingProjectID(t *testing.T) {
	g := NewWithT(t)

	_, err := renderBrokerConfig(&BrokerInfo{Type: BrokerTypeGooglePubSub})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring(BrokerGooglePubSubProjectIDEnv))
}

func TestRenderBrokerConfigUnknownTypeErrors(t *testing.T) {
	g := NewWithT(t)

	_, err := renderBrokerConfig(&BrokerInfo{Type: BrokerType("unknown-broker")})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("unknown"))
}

func TestRenderBrokerConfigIsDeterministic(t *testing.T) {
	g := NewWithT(t)

	info := &BrokerInfo{Type: BrokerTypeRabbitMQ}
	out1, err := renderBrokerConfig(info)
	g.Expect(err).NotTo(HaveOccurred())
	out2, err := renderBrokerConfig(info)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(out1).To(Equal(out2))
}

func TestRenderBrokerConfigGooglePubSubIsValidYAML(t *testing.T) {
	g := NewWithT(t)

	out, err := renderBrokerConfig(&BrokerInfo{
		Type:      BrokerTypeGooglePubSub,
		ProjectID: "my-gcp-project",
	})
	g.Expect(err).NotTo(HaveOccurred())

	// Strip the 2-line header.
	lines := strings.SplitN(out, "\n", 3)
	var parsed brokerFileDocument
	g.Expect(yaml.Unmarshal([]byte(lines[2]), &parsed)).To(Succeed())
	g.Expect(parsed.Broker.Type).To(Equal(BrokerTypeGooglePubSub))
	g.Expect(parsed.Broker.GooglePubSub).NotTo(BeNil())
	g.Expect(parsed.Broker.GooglePubSub.ProjectID).To(Equal("my-gcp-project"))
	g.Expect(parsed.Broker.GooglePubSub.CreateTopicIfMissing).To(BeFalse())
	g.Expect(parsed.Broker.RabbitMQ).To(BeNil())
}

func TestRenderBrokerConfigRabbitMQIsValidYAML(t *testing.T) {
	g := NewWithT(t)

	out, err := renderBrokerConfig(&BrokerInfo{Type: BrokerTypeRabbitMQ})
	g.Expect(err).NotTo(HaveOccurred())

	// Strip the 2-line header.
	lines := strings.SplitN(out, "\n", 3)
	var parsed brokerFileDocument
	g.Expect(yaml.Unmarshal([]byte(lines[2]), &parsed)).To(Succeed())
	g.Expect(parsed.Broker.Type).To(Equal(BrokerTypeRabbitMQ))
	g.Expect(parsed.Broker.RabbitMQ).NotTo(BeNil())
	g.Expect(parsed.Broker.RabbitMQ.URL).NotTo(BeEmpty())
	g.Expect(parsed.Broker.RabbitMQ.ExchangeType).To(Equal("topic"))
	g.Expect(parsed.Broker.GooglePubSub).To(BeNil())
}

// --- brokerTopic tests ---

func TestBrokerTopic(t *testing.T) {
	g := NewWithT(t)

	g.Expect(brokerTopic("hyperfleet-system", "clusters")).To(Equal("hyperfleet-system-clusters"))
	g.Expect(brokerTopic("other-ns", "nodepools")).To(Equal("other-ns-nodepools"))
}
