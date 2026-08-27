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

package api

import (
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

// parseConfig round-trips a rendered document back into a generic map so tests
// assert on the actual serialized keys (the contract the API consumes), not on
// the Go structs.
func parseConfig(g *WithT, out string) map[string]any {
	var m map[string]any
	g.Expect(yaml.Unmarshal([]byte(out), &m)).To(Succeed())
	return m
}

func TestRenderConfigIsDeterministic(t *testing.T) {
	g := NewWithT(t)

	in := configInput{
		AuthEnabled: true,
		Issuer:      testIssuer,
		Audience:    testAudience,
		JWKCertURL:  testJWKCertURL,
		TLSEnabled:  true,
		Entities:    []EntityDescriptor{{Kind: "Cluster", Plural: "clusters"}},
	}

	// Byte-for-byte stability across calls is what makes the content-hash rollout
	// meaningful: identical input must never produce a different document.
	first, err := renderConfig(in)
	g.Expect(err).NotTo(HaveOccurred())
	second, err := renderConfig(in)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).To(Equal(first))

	g.Expect(first).To(HavePrefix(configHeader))
}

func TestRenderConfigURLAndTLS(t *testing.T) {
	g := NewWithT(t)

	out, err := renderConfig(configInput{
		AuthEnabled: true,
		Issuer:      testIssuer,
		Audience:    testAudience,
		JWKCertURL:  testJWKCertURL,
		TLSEnabled:  true,
		Entities:    cloudCAPITestEntities(),
	})
	g.Expect(err).NotTo(HaveOccurred())

	cfg := parseConfig(g, out)

	// Database is never written to the config file — credentials come from env.
	g.Expect(cfg).NotTo(HaveKey("database"))

	server := cfg["server"].(map[string]any)
	g.Expect(server["host"]).To(Equal(bindAllHost))
	g.Expect(server["port"]).To(BeEquivalentTo(portHTTP))

	tls := server["tls"].(map[string]any)
	g.Expect(tls["enabled"]).To(BeTrue())
	g.Expect(tls["cert_file"]).To(Equal(tlsCertFilePath))
	g.Expect(tls["key_file"]).To(Equal(tlsKeyFilePath))

	jwt := server["jwt"].(map[string]any)
	g.Expect(jwt["enabled"]).To(BeTrue())
	configs := jwt["configs"].([]any)
	g.Expect(configs).To(HaveLen(1))
	c0 := configs[0].(map[string]any)
	g.Expect(c0["issuer_url"]).To(Equal(testIssuer))
	g.Expect(c0["audience"]).To(Equal(testAudience))
	g.Expect(c0["jwk_cert_url"]).To(Equal(testJWKCertURL))
	// The file path must not be present in the URL case, and the operator leaves
	// header/identity_claim unset so the API applies its own defaults.
	g.Expect(c0).NotTo(HaveKey("jwk_cert_file"))
	g.Expect(c0).NotTo(HaveKey("header"))
	g.Expect(c0).NotTo(HaveKey("identity_claim"))

	// Entities round-trip with the API's snake_case keys.
	entities := cfg["entities"].([]any)
	g.Expect(entities).To(HaveLen(len(cloudCAPITestEntities())))
	first := entities[0].(map[string]any)
	g.Expect(first["kind"]).To(Equal("Cluster"))
	g.Expect(first["plural"]).To(Equal("clusters"))
	g.Expect(first["spec_schema_name"]).To(Equal("ClusterSpec"))
	g.Expect(first["require_spec_schema"]).To(BeTrue())
}

func TestRenderConfigJWKSFile(t *testing.T) {
	g := NewWithT(t)

	out, err := renderConfig(configInput{
		AuthEnabled: true,
		Issuer:      testIssuer,
		Audience:    testAudience,
		JWKCertFile: jwksFilePath,
	})
	g.Expect(err).NotTo(HaveOccurred())

	c0 := parseConfig(g, out)["server"].(map[string]any)["jwt"].(map[string]any)["configs"].([]any)[0].(map[string]any)
	g.Expect(c0["jwk_cert_file"]).To(Equal(jwksFilePath))
	g.Expect(c0).NotTo(HaveKey("jwk_cert_url"))
}

func TestRenderConfigAuthDisabled(t *testing.T) {
	g := NewWithT(t)

	out, err := renderConfig(configInput{AuthEnabled: false})
	g.Expect(err).NotTo(HaveOccurred())

	cfg := parseConfig(g, out)
	server := cfg["server"].(map[string]any)
	jwt := server["jwt"].(map[string]any)
	g.Expect(jwt["enabled"]).To(BeFalse())
	// No issuer configs and no TLS block when neither is requested.
	g.Expect(jwt).NotTo(HaveKey("configs"))
	g.Expect(server).NotTo(HaveKey("tls"))
	// Empty entity set emits no entities key.
	g.Expect(cfg).NotTo(HaveKey("entities"))
}

func TestRenderConfigErrorsWhenAuthOnButNoJWKSource(t *testing.T) {
	g := NewWithT(t)

	// Guards the controller-to-renderer contract: auth on with neither a URL nor a
	// file is a wiring bug and must fail loudly rather than emit a broken config.
	_, err := renderConfig(configInput{
		AuthEnabled: true,
		Issuer:      testIssuer,
		Audience:    testAudience,
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("no JWKS source"))
}

// cloudCAPITestEntities is a small representative entity set for config tests. It
// mirrors the shape the bundle package supplies without importing it (that would
// be an import cycle).
func cloudCAPITestEntities() []EntityDescriptor {
	return []EntityDescriptor{
		{Kind: "Cluster", Plural: "clusters", SpecSchemaName: "ClusterSpec", RequireSpecSchema: true},
		{Kind: "Channel", Plural: "channels", SpecSchemaName: "ChannelSpec"},
	}
}
