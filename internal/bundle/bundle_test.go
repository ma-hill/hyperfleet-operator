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
	"testing"

	. "github.com/onsi/gomega"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
	"github.com/openshift-hyperfleet/hyperfleet-operator/internal/component/api"
)

func TestEntitiesForBundleCloudCAPI(t *testing.T) {
	g := NewWithT(t)

	ents, err := entitiesForBundle(hyperfleetv1alpha1.BundleCloudCAPI)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ents).To(Equal(cloudCAPIEntities))
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

func TestResolveWiresSharedTierAPIComponent(t *testing.T) {
	g := NewWithT(t)

	const jwks = "https://issuer.example.com/keys"
	comps, err := Resolve(hyperfleetv1alpha1.BundleCloudCAPI, Config{
		APIImage:        "example.com/api:test",
		Namespace:       "hyperfleet-system",
		ResolvedJWKSURL: jwks,
	})
	g.Expect(err).NotTo(HaveOccurred())

	// Phase 1: every bundle resolves to exactly [API].
	g.Expect(comps).To(HaveLen(1))

	comp, ok := comps[0].(*api.Component)
	g.Expect(ok).To(BeTrue())
	g.Expect(comp.Image).To(Equal("example.com/api:test"))
	g.Expect(comp.Namespace).To(Equal("hyperfleet-system"))
	g.Expect(comp.ResolvedJWKSURL).To(Equal(jwks))
	g.Expect(comp.Entities).To(Equal(cloudCAPIEntities))
}

func TestResolveOnPremAgentErrors(t *testing.T) {
	g := NewWithT(t)

	// Resolving the on-prem/agent bundle must fail until it has a real entity
	// set, not silently return an API component with no entities.
	comps, err := Resolve(hyperfleetv1alpha1.BundleOnPremAgent, Config{Namespace: "ns"})
	g.Expect(err).To(HaveOccurred())
	g.Expect(comps).To(BeNil())
}
