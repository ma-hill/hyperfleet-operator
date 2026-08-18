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

package controller

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
)

// Repeated fixture values are declared once as constants: goconst flags any
// string literal that recurs three or more times in a package, and a single
// definition keeps the specs in sync.
const (
	testDBSecretName = "hyperfleet-db"
	testIssuerURL    = "https://issuer.example.com"
	testAudience     = "hyperfleet-api"
)

// These specs exercise the CRD's declarative validation and defaulting through a
// real API server (envtest). They assert the schema contract for HYPERFLEET-1406;
// no reconciler behavior is involved.

// validHyperFleetConfig returns a well-formed, minimally-complete singleton that
// the API server must accept. Individual specs mutate a copy to drive negative
// cases.
func validHyperFleetConfig() *hyperfleetv1alpha1.HyperFleetConfig {
	return &hyperfleetv1alpha1.HyperFleetConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: hyperfleetv1alpha1.SingletonName,
		},
		Spec: hyperfleetv1alpha1.HyperFleetConfigSpec{
			Bundle: hyperfleetv1alpha1.BundleCloudCAPI,
			API: hyperfleetv1alpha1.APISpec{
				Database: hyperfleetv1alpha1.DatabaseSpec{
					SecretRef: hyperfleetv1alpha1.SecretReference{Name: testDBSecretName},
				},
				Auth: hyperfleetv1alpha1.AuthSpec{
					Enabled:  ptr.To(true),
					Issuer:   testIssuerURL,
					Audience: testAudience,
				},
			},
		},
	}
}

var _ = Describe("HyperFleetConfig CRD validation", func() {
	ctx := context.Background()

	// Every valid object is forced to the same name ("cluster"), so specs collide
	// unless the singleton is torn down between them.
	AfterEach(func() {
		obj := &hyperfleetv1alpha1.HyperFleetConfig{
			ObjectMeta: metav1.ObjectMeta{Name: hyperfleetv1alpha1.SingletonName},
		}
		// A negative spec may have prevented creation, so NotFound is a valid
		// "already clean" outcome; any other delete error must fail the test
		// loudly rather than be masked as an Eventually timeout below.
		if err := k8sClient.Delete(ctx, obj); err != nil && !errors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		Eventually(func() bool {
			err := k8sClient.Get(ctx,
				types.NamespacedName{Name: hyperfleetv1alpha1.SingletonName},
				&hyperfleetv1alpha1.HyperFleetConfig{})
			return errors.IsNotFound(err)
		}).Should(BeTrue())
	})

	Context("singleton enforcement", func() {
		It("accepts the resource when named 'cluster'", func() {
			Expect(k8sClient.Create(ctx, validHyperFleetConfig())).To(Succeed())
		})

		It("rejects any name other than 'cluster'", func() {
			obj := validHyperFleetConfig()
			obj.Name = "not-cluster"
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("only permitted name is 'cluster'"))
		})

		It("rejects a second instance as AlreadyExists", func() {
			// The name-pin CEL rule forces every instance to the name "cluster";
			// cluster-scoped name uniqueness then makes the second create collide.
			Expect(k8sClient.Create(ctx, validHyperFleetConfig())).To(Succeed())
			err := k8sClient.Create(ctx, validHyperFleetConfig())
			Expect(err).To(HaveOccurred())
			Expect(errors.IsAlreadyExists(err)).To(BeTrue())
		})
	})

	Context("enum validation", func() {
		// Positive coverage: every constant declared in the Go source must be
		// accepted by the generated enum. Generating one spec per value guards
		// against the constants and the +kubebuilder:validation:Enum marker drifting
		// apart.
		for _, bundle := range hyperfleetv1alpha1.AllBundleTypes {
			It("accepts declared bundle value "+string(bundle), func() {
				obj := validHyperFleetConfig()
				obj.Spec.Bundle = bundle
				Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			})
		}

		for _, profile := range hyperfleetv1alpha1.AllSizingProfiles {
			It("accepts declared sizing profile "+string(profile), func() {
				obj := validHyperFleetConfig()
				obj.Spec.API.Profile = profile
				Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			})
		}

		It("rejects an unknown bundle", func() {
			obj := validHyperFleetConfig()
			obj.Spec.Bundle = "bogus-bundle"
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Unsupported value"))
		})

		It("rejects an unknown sizing profile", func() {
			obj := validHyperFleetConfig()
			obj.Spec.API.Profile = "huge"
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Unsupported value"))
		})
	})

	Context("bundle immutability", func() {
		It("rejects changing the bundle after creation", func() {
			obj := validHyperFleetConfig()
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			obj.Spec.Bundle = hyperfleetv1alpha1.BundleOnPremAgent
			err := k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bundle is immutable"))
		})

		It("allows changing a mutable field (profile) after creation", func() {
			// The immutability rule is scoped to bundle; other fields must remain
			// updatable. This proves self==oldSelf was not applied too broadly.
			obj := validHyperFleetConfig()
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			obj.Spec.API.Profile = hyperfleetv1alpha1.SizingProfileLarge
			Expect(k8sClient.Update(ctx, obj)).To(Succeed())

			got := &hyperfleetv1alpha1.HyperFleetConfig{}
			Expect(k8sClient.Get(ctx,
				types.NamespacedName{Name: hyperfleetv1alpha1.SingletonName}, got)).To(Succeed())
			Expect(got.Spec.API.Profile).To(Equal(hyperfleetv1alpha1.SizingProfileLarge))
		})
	})

	Context("auth validation", func() {
		It("rejects enabled auth without issuer and audience", func() {
			obj := validHyperFleetConfig()
			obj.Spec.API.Auth = hyperfleetv1alpha1.AuthSpec{Enabled: ptr.To(true)}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("issuer and audience are required when auth is enabled"))
		})

		It("rejects a non-https issuer", func() {
			obj := validHyperFleetConfig()
			obj.Spec.API.Auth.Issuer = "http://issuer.example.com"
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("issuer must be a valid https URL"))
		})

		It("rejects a scheme-only issuer with no host", func() {
			// "https://" satisfies a naive startsWith('https://') check but has no
			// host; the URL-library rule must reject it.
			obj := validHyperFleetConfig()
			obj.Spec.API.Auth.Issuer = "https://"
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("issuer must be a valid https URL"))
		})

		It("rejects a present but empty issuer", func() {
			// An explicit empty issuer is "present" to CEL, so MinLength (not the
			// https XValidation alone) must reject it. Only unstructured can carry an
			// explicit empty scalar; the typed client omits it via omitempty.
			u := minimalUnstructured()
			auth := u.Object["spec"].(map[string]interface{})["api"].(map[string]interface{})["auth"].(map[string]interface{})
			auth["issuer"] = ""
			err := k8sClient.Create(ctx, u)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("should be at least 1 chars long"))
		})

		It("rejects an issuer longer than the 2048-character maximum", func() {
			// A long-but-valid https URL isolates MaxLength: isURL, scheme and host
			// all pass, so only the length bound can reject it.
			obj := validHyperFleetConfig()
			obj.Spec.API.Auth.Issuer = testIssuerURL + "/" + strings.Repeat("a", 2048)
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("may not be more than 2048 bytes"))
		})

		It("rejects an empty audience when auth is enabled", func() {
			// An explicit empty string is "present" to CEL has(), so the object-level
			// rule alone would let it through; MinLength must reject it. Only an
			// unstructured object can carry an explicit empty scalar (the typed client
			// omits it via omitempty).
			u := minimalUnstructured()
			u.Object["spec"].(map[string]interface{})["api"].(map[string]interface{})["auth"] =
				map[string]interface{}{
					"enabled":  true,
					"issuer":   testIssuerURL,
					"audience": "",
				}
			err := k8sClient.Create(ctx, u)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("should be at least 1 chars long"))
		})

		It("rejects an audience longer than the 253-character maximum", func() {
			obj := validHyperFleetConfig()
			obj.Spec.API.Auth.Audience = strings.Repeat("a", 254)
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("may not be more than 253 bytes"))
		})

		It("accepts disabled auth without issuer and audience", func() {
			obj := validHyperFleetConfig()
			obj.Spec.API.Auth = hyperfleetv1alpha1.AuthSpec{Enabled: ptr.To(false)}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		})
	})

	Context("required fields", func() {
		It("rejects an empty database secret name", func() {
			obj := validHyperFleetConfig()
			obj.Spec.API.Database.SecretRef.Name = ""
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			// Assert the MinLength message specifically: an empty name also trips the
			// DNS-1123 Pattern, so checking only the field path would keep passing if
			// MinLength=1 were dropped. This isolates the MinLength constraint.
			Expect(err.Error()).To(ContainSubstring("should be at least 1 chars long"))
		})

		It("rejects a database secret name that is not a DNS-1123 subdomain", func() {
			obj := validHyperFleetConfig()
			obj.Spec.API.Database.SecretRef.Name = "Invalid_Name"
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("database.secretRef.name"))
		})

		It("rejects a database secret name longer than the 253-character maximum", func() {
			// A 254-char all-lowercase name satisfies the DNS-1123 Pattern, so only
			// MaxLength can reject it.
			obj := validHyperFleetConfig()
			obj.Spec.API.Database.SecretRef.Name = strings.Repeat("a", 254)
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("may not be more than 253 bytes"))
		})
	})

	Context("optional TLS", func() {
		It("accepts a well-formed TLS secret reference", func() {
			obj := validHyperFleetConfig()
			obj.Spec.API.TLS = &hyperfleetv1alpha1.TLSSpec{
				SecretRef: hyperfleetv1alpha1.SecretReference{Name: "hyperfleet-tls"},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		})

		It("rejects a TLS block with an empty secret name", func() {
			obj := validHyperFleetConfig()
			obj.Spec.API.TLS = &hyperfleetv1alpha1.TLSSpec{
				SecretRef: hyperfleetv1alpha1.SecretReference{Name: ""},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("tls.secretRef.name"))
		})

		It("rejects a TLS secret name longer than the 253-character maximum", func() {
			obj := validHyperFleetConfig()
			obj.Spec.API.TLS = &hyperfleetv1alpha1.TLSSpec{
				SecretRef: hyperfleetv1alpha1.SecretReference{Name: strings.Repeat("a", 254)},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("may not be more than 253 bytes"))
		})
	})

	Context("required object fields", func() {
		// Structural-schema Required markers must reject an object that omits a
		// mandatory field entirely. Only an unstructured object can omit fields the
		// typed client always sends.
		It("rejects a config with no bundle", func() {
			u := minimalUnstructured()
			delete(u.Object["spec"].(map[string]interface{}), "bundle")
			err := k8sClient.Create(ctx, u)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.bundle"))
		})

		It("rejects a config with no api", func() {
			u := minimalUnstructured()
			delete(u.Object["spec"].(map[string]interface{}), "api")
			err := k8sClient.Create(ctx, u)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.api"))
		})

		It("rejects an api with no database", func() {
			u := minimalUnstructured()
			api := u.Object["spec"].(map[string]interface{})["api"].(map[string]interface{})
			delete(api, "database")
			err := k8sClient.Create(ctx, u)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("database"))
		})

		It("rejects an api with no auth", func() {
			u := minimalUnstructured()
			api := u.Object["spec"].(map[string]interface{})["api"].(map[string]interface{})
			delete(api, "auth")
			err := k8sClient.Create(ctx, u)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("auth"))
		})
	})

	Context("defaulting", func() {
		// Server-side defaults only apply to fields genuinely absent from the
		// submitted object. An unstructured object is used to omit them entirely.
		It("populates profile and auth.enabled defaults on a minimal object", func() {
			u := minimalUnstructured()
			Expect(k8sClient.Create(ctx, u)).To(Succeed())

			got := &hyperfleetv1alpha1.HyperFleetConfig{}
			Expect(k8sClient.Get(ctx,
				types.NamespacedName{Name: hyperfleetv1alpha1.SingletonName}, got)).To(Succeed())
			Expect(got.Spec.API.Profile).To(Equal(hyperfleetv1alpha1.SizingProfileSmall))
			Expect(got.Spec.API.Auth.Enabled).To(HaveValue(BeTrue()))
		})

		It("defaults auth.enabled to true on the typed-client path when omitted", func() {
			// Regression guard: with a non-pointer bool the typed client serializes
			// enabled:false when the field is left zero, silently defeating the
			// default=true. A *bool with omitempty must round-trip to true here.
			obj := validHyperFleetConfig()
			obj.Spec.API.Auth.Enabled = nil
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			got := &hyperfleetv1alpha1.HyperFleetConfig{}
			Expect(k8sClient.Get(ctx,
				types.NamespacedName{Name: hyperfleetv1alpha1.SingletonName}, got)).To(Succeed())
			Expect(got.Spec.API.Auth.Enabled).To(HaveValue(BeTrue()))
		})
	})
})

// minimalUnstructured builds the smallest valid HyperFleetConfig as an
// unstructured object, omitting every field that carries a server-side default
// (auth.enabled, api.profile) so those defaults are exercised.
func minimalUnstructured() *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(hyperfleetv1alpha1.GroupVersion.WithKind("HyperFleetConfig"))
	u.SetName(hyperfleetv1alpha1.SingletonName)
	u.Object["spec"] = map[string]interface{}{
		"bundle": string(hyperfleetv1alpha1.BundleCloudCAPI),
		"api": map[string]interface{}{
			"database": map[string]interface{}{
				"secretRef": map[string]interface{}{"name": testDBSecretName},
			},
			"auth": map[string]interface{}{
				// enabled omitted -> defaults to true
				"issuer":   testIssuerURL,
				"audience": testAudience,
			},
			// profile omitted -> defaults to "small"
		},
	}
	return u
}
