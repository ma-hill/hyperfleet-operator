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

package bundle

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
	"github.com/openshift-hyperfleet/hyperfleet-operator/internal/component/api"
	"github.com/openshift-hyperfleet/hyperfleet-operator/internal/component/sentinel"
)

var testConfig = Config{
	APIImage:      "test-quay.io/api:test",
	SentinelImage: "test-quay.io/sentinel:test",
	Namespace:     "hyperfleet-system",
	BrokerInfo: &sentinel.BrokerInfo{
		Type: sentinel.BrokerTypeRabbitMQ,
	},
}

func TestEntitiesForBundleCloudCAPI(t *testing.T) {
	g := NewWithT(t)

	ents, err := entitiesForBundle(hyperfleetv1alpha1.BundleCloudCAPI)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ents).To(Equal(CloudCAPIEntities))
	g.Expect(ents).NotTo(BeEmpty())

	// Sanity: the cloud-capi set registers the core entities the API expects for
	// this flavor.
	kinds := map[string]bool{}
	for _, e := range ents {
		kinds[e.Kind] = true
	}
	g.Expect(kinds).To(HaveKey("Cluster"))
	g.Expect(kinds).To(HaveKey("NodePool"))
}

func TestEntitiesForBundleOnPremAgentErrors(t *testing.T) {
	g := NewWithT(t)

	// The on-prem/agent bundle has no entity set yet: resolving it must fail
	// loudly rather than silently produce an entities-free (so routes-free) API
	// that still reports healthy.
	ents, err := entitiesForBundle(hyperfleetv1alpha1.BundleOnPremAgent)
	g.Expect(err).To(HaveOccurred())
	g.Expect(ents).To(BeNil())
}

func TestEntitiesForBundleUnknownErrors(t *testing.T) {
	g := NewWithT(t)

	// An unrecognized bundle falls through the switch default and errors rather
	// than silently defaulting to cloud-capi or an empty set.
	ents, err := entitiesForBundle(hyperfleetv1alpha1.BundleType("does-not-exist"))
	g.Expect(err).To(HaveOccurred())
	g.Expect(ents).To(BeNil())
}

func TestResolveWiresSharedTierComponents(t *testing.T) {
	g := NewWithT(t)

	comps, err := Resolve(hyperfleetv1alpha1.BundleCloudCAPI, testConfig)
	g.Expect(err).NotTo(HaveOccurred())

	// 1 API + 5 sentinel instances (one per cloud-capi entity).
	g.Expect(comps).To(HaveLen(1 + len(CloudCAPIEntities)))

	apiComp, ok := comps[0].(*api.Component)
	g.Expect(ok).To(BeTrue())
	g.Expect(apiComp.Image).To(Equal("test-quay.io/api:test"))
	g.Expect(apiComp.Namespace).To(Equal("hyperfleet-system"))
	g.Expect(apiComp.Entities).To(Equal(CloudCAPIEntities))

	expectedTypes := make([]string, len(CloudCAPIEntities))
	for i, e := range CloudCAPIEntities {
		expectedTypes[i] = e.Plural
	}
	for i, rt := range expectedTypes {
		sc, ok := comps[1+i].(*sentinel.Component)
		g.Expect(ok).To(BeTrue(), "comps[%d] should be a sentinel.Component", 1+i)
		g.Expect(sc.ResourceType).To(Equal(rt))
		g.Expect(sc.Image).To(Equal("test-quay.io/sentinel:test"))
		g.Expect(sc.Namespace).To(Equal("hyperfleet-system"))
		g.Expect(sc.BrokerInfo.Type).To(Equal(sentinel.BrokerTypeRabbitMQ))
	}
}

func TestSetResolvedJWKSURL(t *testing.T) {
	g := NewWithT(t)

	comps, err := Resolve(hyperfleetv1alpha1.BundleCloudCAPI, testConfig)
	g.Expect(err).NotTo(HaveOccurred())

	apiComp, ok := comps[0].(*api.Component)
	g.Expect(ok).To(BeTrue())
	g.Expect(apiComp.ResolvedJWKSURL).To(BeEmpty())
	SetResolvedJWKSURL(comps, "https://issuer.example.com/.well-known/jwks.json")
	g.Expect(apiComp.ResolvedJWKSURL).To(Equal("https://issuer.example.com/.well-known/jwks.json"))
}

func TestResolveOnPremAgentErrors(t *testing.T) {
	g := NewWithT(t)

	// Resolving the on-prem/agent bundle must fail until it has a real entity
	// set, not silently return an API component with no entities.
	comps, err := Resolve(hyperfleetv1alpha1.BundleOnPremAgent, testConfig)
	g.Expect(err).To(HaveOccurred())
	g.Expect(comps).To(BeNil())
}

// TestEveryComponentReportsConditions is the implementation-time enforcement
// of the Conditions() contract in docs/component-pattern.md: every component
// in every resolvable bundle must return at least one condition when handed
// its own freshly rendered objects — which carry no live status yet, so this
// also proves the pessimistic path (health undeterminable) still reports
// rather than returning empty. The runtime behavior for an empty report is
// only a controller log line and an absent rollup entry, so a violation must
// be caught here, at build time, not in a cluster.
func TestEveryComponentReportsConditions(t *testing.T) {
	g := NewWithT(t)

	// A renderable CR: auth with a pinned JWKS cert Secret so nothing needs
	// controller-supplied OIDC discovery.
	cr := &hyperfleetv1alpha1.HyperFleetConfig{
		ObjectMeta: metav1.ObjectMeta{Name: hyperfleetv1alpha1.SingletonName},
		Spec: hyperfleetv1alpha1.HyperFleetConfigSpec{
			Bundle: hyperfleetv1alpha1.BundleCloudCAPI,
			API: hyperfleetv1alpha1.APISpec{
				Database: hyperfleetv1alpha1.DatabaseSpec{
					SecretRef: hyperfleetv1alpha1.SecretReference{Name: "hyperfleet-db"},
				},
				Auth: hyperfleetv1alpha1.AuthSpec{
					Enabled:          ptr.To(true),
					Issuer:           "https://issuer.example.com",
					Audience:         "hyperfleet-api",
					JWKCertSecretRef: &hyperfleetv1alpha1.SecretReference{Name: "hyperfleet-jwks"},
				},
			},
		},
	}

	comps, err := Resolve(hyperfleetv1alpha1.BundleCloudCAPI, testConfig)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(comps).NotTo(BeEmpty())

	ctx := context.Background()
	for _, comp := range comps {
		objs, err := comp.Render(ctx, cr)
		g.Expect(err).NotTo(HaveOccurred(), "component %q must render", comp.Name())

		conds, err := comp.Conditions(ctx, cr, objs)
		g.Expect(err).NotTo(HaveOccurred(), "component %q Conditions must not error", comp.Name())
		g.Expect(conds).NotTo(BeEmpty(),
			"component %q must report at least one condition (an Available condition) even when health cannot be determined — see docs/component-pattern.md",
			comp.Name())
	}
}
