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

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
	"github.com/openshift-hyperfleet/hyperfleet-operator/internal/component"
)

func parseBrokerConfig(t *testing.T, out string) map[string]any {
	t.Helper()
	g := NewWithT(t)
	var m map[string]any
	g.Expect(yaml.Unmarshal([]byte(out), &m)).To(Succeed())
	return m
}

func TestRenderBrokerConfigRabbitMQHasURLKey(t *testing.T) {
	g := NewWithT(t)

	out, err := renderBrokerConfig(BrokerTypeRabbitMQ)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(out).To(HavePrefix(brokerConfigHeader))

	broker := parseBrokerConfig(t, out)["broker"].(map[string]any)
	g.Expect(broker["type"]).To(Equal(string(BrokerTypeRabbitMQ)))

	rmq := broker["rabbitmq"].(map[string]any)
	g.Expect(rmq).To(HaveKey("url"), "rabbitmq.url must be present so viper can bind BROKER_RABBITMQ_URL")
	g.Expect(rmq["exchange_type"]).To(Equal(brokerExchangeType))
}

func TestRenderBrokerConfigGooglePubSub(t *testing.T) {
	g := NewWithT(t)

	t.Setenv(BrokerGooglePubSubProjectIDEnv, "my-project")

	out, err := renderBrokerConfig(BrokerTypeGooglePubSub)
	g.Expect(err).NotTo(HaveOccurred())

	broker := parseBrokerConfig(t, out)["broker"].(map[string]any)
	g.Expect(broker["type"]).To(Equal(string(BrokerTypeGooglePubSub)))
	g.Expect(broker).NotTo(HaveKey("rabbitmq"))

	ps := broker["googlepubsub"].(map[string]any)
	g.Expect(ps["project_id"]).To(Equal("my-project"))
	g.Expect(ps["create_topic_if_missing"]).To(BeFalse())
}

func TestRenderBrokerConfigGooglePubSubRequiresProjectID(t *testing.T) {
	g := NewWithT(t)

	t.Setenv(BrokerGooglePubSubProjectIDEnv, "")

	_, err := renderBrokerConfig(BrokerTypeGooglePubSub)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring(BrokerGooglePubSubProjectIDEnv))
}

func TestRenderBrokerConfigUnknownType(t *testing.T) {
	g := NewWithT(t)

	_, err := renderBrokerConfig("broker-nonexistent-type")
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("broker-nonexistent-type"))
}

func TestRenderConfigIsDeterministic(t *testing.T) {
	g := NewWithT(t)

	in := configInput{
		ResourceType: "clusters",
		APIBaseURL:   "http://hyperfleet-api:8000",
		BrokerTopic:  "hyperfleet-system-clusters",
	}

	first, err := renderConfig(in)
	g.Expect(err).NotTo(HaveOccurred())
	second, err := renderConfig(in)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).To(Equal(first))

	g.Expect(first).To(HavePrefix(configHeader))
}

func TestReferencedSecretsRabbitMQ(t *testing.T) {
	g := NewWithT(t)

	c := &Component{Options: Options{BrokerType: BrokerTypeRabbitMQ}}
	cr := &hyperfleetv1alpha1.HyperFleetConfig{}

	refs := c.ReferencedSecrets(cr)
	g.Expect(refs).To(Equal([]component.SecretReference{
		{Role: "broker", SecretName: BrokerSecretName},
	}))
}

func TestReferencedSecretsGooglePubSub(t *testing.T) {
	g := NewWithT(t)

	c := &Component{Options: Options{BrokerType: BrokerTypeGooglePubSub}}
	cr := &hyperfleetv1alpha1.HyperFleetConfig{}

	refs := c.ReferencedSecrets(cr)
	g.Expect(refs).To(BeEmpty())
}
