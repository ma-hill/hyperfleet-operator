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
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
	apicomponent "github.com/openshift-hyperfleet/hyperfleet-operator/internal/component/api"
)

// These are plain unit tests (no envtest): OIDC discovery, the content-hash, and
// the Secret→request mapping are all pure or use an in-process HTTP server, so
// they need no API server.

// Hash-entry ids reused across TestComputeConfigHashProperties and
// TestStampConfigHashSetsAnnotation.
const (
	dbHostHashID     = "database/db.host"
	dbPasswordHashID = "database/db.password"
)

// discoveryCR returns an auth-enabled CR with no pinned JWKS source, so
// resolveJWKSURL falls through to OIDC discovery against the given issuer.
func discoveryCR(issuer string) *hyperfleetv1alpha1.HyperFleetConfig {
	return &hyperfleetv1alpha1.HyperFleetConfig{
		ObjectMeta: metav1.ObjectMeta{Name: hyperfleetv1alpha1.SingletonName},
		Spec: hyperfleetv1alpha1.HyperFleetConfigSpec{
			Bundle: hyperfleetv1alpha1.BundleCloudCAPI,
			API: hyperfleetv1alpha1.APISpec{
				Auth: hyperfleetv1alpha1.AuthSpec{
					Enabled:  ptr.To(true),
					Issuer:   issuer,
					Audience: "hyperfleet-api",
				},
			},
		},
	}
}

func TestResolveJWKSURLDiscoversFromIssuer(t *testing.T) {
	g := NewWithT(t)

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		// The issuer in the document must match the one we asked for (the server's
		// own URL); "http://"+r.Host reconstructs it for the httptest server.
		_, _ = w.Write([]byte(`{"issuer":"http://` + r.Host + `","jwks_uri":"https://issuer.example.com/keys"}`))
	}))
	defer srv.Close()

	r := &HyperFleetConfigReconciler{HTTPClient: srv.Client()}
	url, err := r.resolveJWKSURL(context.Background(), discoveryCR(srv.URL))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(url).To(Equal("https://issuer.example.com/keys"))
	g.Expect(gotPath).To(Equal("/.well-known/openid-configuration"))
}

func TestDiscoverJWKSURLRejectsIssuerMismatch(t *testing.T) {
	g := NewWithT(t)

	// 200 with a well-formed https jwks_uri but an issuer that does not match the
	// one we asked for: the document is untrusted and must be rejected before its
	// jwks_uri is used (OIDC Discovery 1.0 §4.3).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"https://evil.example.com","jwks_uri":"https://evil.example.com/keys"}`))
	}))
	defer srv.Close()

	r := &HyperFleetConfigReconciler{HTTPClient: srv.Client()}
	_, err := r.discoverJWKSURL(context.Background(), srv.URL)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("issuer"))
}

func TestDiscoverJWKSURLRejectsNonHTTPS(t *testing.T) {
	g := NewWithT(t)

	// Matching issuer but a plaintext jwks_uri: retrieving signing keys over http
	// is MITM-able, so the discovered URL must be rejected even though the pinned
	// jwkCertURL is the only one the CRD guards.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"http://` + r.Host + `","jwks_uri":"http://insecure.example.com/keys"}`))
	}))
	defer srv.Close()

	r := &HyperFleetConfigReconciler{HTTPClient: srv.Client()}
	_, err := r.discoverJWKSURL(context.Background(), srv.URL)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("non-https"))
}

func TestResolveJWKSURLSkipsDiscoveryWhenPinned(t *testing.T) {
	g := NewWithT(t)

	// A client pointed at a closed server would error if discovery were attempted;
	// with a pinned URL it must never be called.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	srv.Close()

	r := &HyperFleetConfigReconciler{HTTPClient: srv.Client()}

	cr := discoveryCR(srv.URL)
	cr.Spec.API.Auth.JWKCertURL = "https://issuer.example.com/certs"
	url, err := r.resolveJWKSURL(context.Background(), cr)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(url).To(BeEmpty(), "pinned URL: renderer reads it from the CR, no discovery")

	// Auth disabled: also no discovery.
	off := discoveryCR(srv.URL)
	off.Spec.API.Auth.Enabled = ptr.To(false)
	url, err = r.resolveJWKSURL(context.Background(), off)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(url).To(BeEmpty())
}

func TestDiscoverJWKSURLErrors(t *testing.T) {
	g := NewWithT(t)

	// Non-200.
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFound.Close()
	r := &HyperFleetConfigReconciler{HTTPClient: notFound.Client()}
	_, err := r.discoverJWKSURL(context.Background(), notFound.URL)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("status 404"))

	// 200 with a matching issuer but no jwks_uri (issuer is validated first, so it
	// must match to reach the missing-jwks_uri error).
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"http://` + r.Host + `"}`))
	}))
	defer empty.Close()
	r = &HyperFleetConfigReconciler{HTTPClient: empty.Client()}
	_, err = r.discoverJWKSURL(context.Background(), empty.URL)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("no jwks_uri"))
}

func TestComputeConfigHashProperties(t *testing.T) {
	g := NewWithT(t)

	base := []hashEntry{
		{id: dbHostHashID, present: true, value: []byte("db.example.com")},
		{id: dbPasswordHashID, present: true, value: []byte("s3cret")},
	}

	h := computeConfigHash("config-a", base)
	g.Expect(h).To(HaveLen(64)) // hex-encoded SHA-256

	// Stable and order-independent (entries are sorted by id internally).
	reordered := []hashEntry{base[1], base[0]}
	g.Expect(computeConfigHash("config-a", reordered)).To(Equal(h))

	// Config change → different hash.
	g.Expect(computeConfigHash("config-b", base)).NotTo(Equal(h))

	// Secret value change (rotation) → different hash.
	rotated := []hashEntry{base[0], {id: dbPasswordHashID, present: true, value: []byte("rotated")}}
	g.Expect(computeConfigHash("config-a", rotated)).NotTo(Equal(h))

	// Absent vs present-empty must differ (the present/absent discriminator).
	absent := []hashEntry{base[0], {id: dbPasswordHashID, present: false}}
	presentEmpty := []hashEntry{base[0], {id: dbPasswordHashID, present: true, value: []byte("")}}
	g.Expect(computeConfigHash("config-a", absent)).NotTo(Equal(computeConfigHash("config-a", presentEmpty)))

	// A present value equal to the bytes of any absent marker must still differ
	// from a genuinely absent datum: the discriminator byte keeps them distinct so
	// no secret value can masquerade as "absent".
	presentAbsentLiteral := []hashEntry{base[0], {id: dbPasswordHashID, present: true, value: []byte("<absent>")}}
	g.Expect(computeConfigHash("config-a", absent)).NotTo(Equal(computeConfigHash("config-a", presentAbsentLiteral)))
}

func TestStampConfigHashSetsAnnotation(t *testing.T) {
	g := NewWithT(t)

	cr := &hyperfleetv1alpha1.HyperFleetConfig{
		ObjectMeta: metav1.ObjectMeta{Name: hyperfleetv1alpha1.SingletonName},
		Spec: hyperfleetv1alpha1.HyperFleetConfigSpec{
			Bundle: hyperfleetv1alpha1.BundleCloudCAPI,
			API: hyperfleetv1alpha1.APISpec{
				Database: hyperfleetv1alpha1.DatabaseSpec{
					SecretRef: hyperfleetv1alpha1.SecretReference{Name: testDBSecretName},
				},
				Auth: hyperfleetv1alpha1.AuthSpec{
					Enabled:    ptr.To(true),
					Issuer:     "https://issuer.example.com",
					Audience:   "hyperfleet-api",
					JWKCertURL: "https://issuer.example.com/certs",
				},
			},
		},
	}

	render := func() []client.Object {
		objs, err := apicomponent.New("img", "hyperfleet-system", apicomponent.Options{}).Render(context.Background(), cr)
		g.Expect(err).NotTo(HaveOccurred())
		return objs
	}

	entries := []hashEntry{{id: dbHostHashID, present: true, value: []byte("h1")}}
	objs := render()
	stampConfigHash(objs, entries)

	depOf := func(objs []client.Object) *appsv1.Deployment {
		for _, o := range objs {
			if d, ok := o.(*appsv1.Deployment); ok {
				return d
			}
		}
		return nil
	}

	dep := depOf(objs)
	g.Expect(dep).NotTo(BeNil())
	got := dep.Spec.Template.Annotations[configHashAnnotation]
	g.Expect(got).NotTo(BeEmpty())

	// Re-stamping the same render with the same entries yields the same hash.
	objs2 := render()
	stampConfigHash(objs2, entries)
	g.Expect(depOf(objs2).Spec.Template.Annotations[configHashAnnotation]).To(Equal(got))

	// A rotated secret value changes the stamped hash.
	objs3 := render()
	stampConfigHash(objs3, []hashEntry{{id: dbHostHashID, present: true, value: []byte("h2")}})
	g.Expect(depOf(objs3).Spec.Template.Annotations[configHashAnnotation]).NotTo(Equal(got))
}

func TestMapSecretToConfig(t *testing.T) {
	g := NewWithT(t)

	r := &HyperFleetConfigReconciler{OperatorNamespace: "hyperfleet-system"}

	inNS := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "hyperfleet-db", Namespace: "hyperfleet-system"}}
	reqs := r.mapSecretToConfig(context.Background(), inNS)
	g.Expect(reqs).To(HaveLen(1))
	g.Expect(reqs[0].Name).To(Equal(hyperfleetv1alpha1.SingletonName))

	other := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "hyperfleet-db", Namespace: "elsewhere"}}
	g.Expect(r.mapSecretToConfig(context.Background(), other)).To(BeEmpty())
}
