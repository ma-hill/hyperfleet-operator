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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
	apicomponent "github.com/openshift-hyperfleet/hyperfleet-operator/internal/component/api"
)

// This file holds the two cluster/network-dependent pieces of reconciliation that
// cannot live in the pure component renderer (HYPERFLEET-1408):
//
//   - OIDC discovery of the JWKS URL, and
//   - the content-hash rollout: reading referenced Secret data and stamping a
//     hash on the Deployment pod template so config or secret-value changes roll
//     the pods (the Helm `checksum/config` pattern, extended to Secret data).

// configHashAnnotation is stamped on the API Deployment's pod template. When it
// changes, the Deployment controller performs a rolling update, so a config or
// referenced-secret change takes effect even though the container image is
// unchanged.
const configHashAnnotation = "hyperfleet.redhat.com/config-hash"

// oidcDiscoveryPath is the well-known suffix appended to an issuer to fetch its
// OpenID provider metadata, per OpenID Connect Discovery 1.0.
const oidcDiscoveryPath = "/.well-known/openid-configuration"

// discoveryTimeout bounds a single OIDC discovery HTTP request.
const discoveryTimeout = 10 * time.Second

// resolveJWKSURL returns the JWKS URL the renderer should write into config.yaml,
// or "" when none is needed. It performs OIDC discovery only in the case the CR
// pins no source: auth on, and neither jwkCertURL nor jwkCertSecretRef set. When
// the CR pins a URL, the renderer uses it directly; when it pins a Secret, the
// renderer uses the mounted file; when auth is off, no JWKS is needed.
func (r *HyperFleetConfigReconciler) resolveJWKSURL(ctx context.Context, cr *hyperfleetv1alpha1.HyperFleetConfig) (string, error) {
	if !apicomponent.AuthEnabled(cr) {
		return "", nil
	}
	a := cr.Spec.API.Auth
	if a.JWKCertURL != "" || a.JWKCertSecretRef != nil {
		return "", nil
	}
	return r.discoverJWKSURL(ctx, a.Issuer)
}

// discoverJWKSURL fetches {issuer}/.well-known/openid-configuration and returns
// its jwks_uri. Any transport error, non-200 status, malformed body, missing
// jwks_uri, issuer mismatch, or non-https jwks_uri is returned as an error so
// Reconcile requeues (a Degraded condition for persistent failures is
// HYPERFLEET-1512).
//
// Two security checks mirror the guarantees the CRD enforces on a pinned
// jwkCertURL, which OIDC discovery would otherwise bypass:
//   - issuer match: the discovery document's `issuer` MUST equal the configured
//     issuer (OIDC Discovery 1.0 §4.3). Without it, a redirected or spoofed
//     endpoint could bind the configured issuer to another issuer's signing keys.
//   - https jwks_uri: the returned URL MUST be https with a host, so a hostile
//     document cannot downgrade key retrieval to plaintext (MITM) or point the
//     API at an internal/attacker-controlled endpoint (SSRF).
func (r *HyperFleetConfigReconciler) discoverJWKSURL(ctx context.Context, issuer string) (string, error) {
	// Per the OIDC Discovery spec the well-known path is appended to the issuer
	// with any single trailing slash collapsed.
	normalizedIssuer := strings.TrimSuffix(issuer, "/")
	discoveryURL := normalizedIssuer + oidcDiscoveryPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", fmt.Errorf("build discovery request for %q: %w", discoveryURL, err)
	}
	req.Header.Set("Accept", "application/json")

	httpClient := r.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: discoveryTimeout}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %q: %w", discoveryURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discovery %q returned status %d", discoveryURL, resp.StatusCode)
	}

	// Bound the read so a hostile or misconfigured endpoint cannot stream an
	// unbounded body into memory; provider metadata is small.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read discovery body from %q: %w", discoveryURL, err)
	}

	var doc struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("parse discovery document from %q: %w", discoveryURL, err)
	}

	// The document is untrusted until its issuer matches the one we asked for; a
	// single trailing slash on either side is not significant.
	if strings.TrimSuffix(doc.Issuer, "/") != normalizedIssuer {
		return "", fmt.Errorf("discovery document from %q has issuer %q, want %q", discoveryURL, doc.Issuer, normalizedIssuer)
	}

	if doc.JWKSURI == "" {
		return "", fmt.Errorf("discovery document from %q has no jwks_uri", discoveryURL)
	}

	// Enforce the same https guarantee the CRD applies to a pinned jwkCertURL.
	u, err := url.Parse(doc.JWKSURI)
	if err != nil {
		return "", fmt.Errorf("discovery document from %q has malformed jwks_uri %q: %w", discoveryURL, doc.JWKSURI, err)
	}
	if u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("discovery document from %q has non-https jwks_uri %q", discoveryURL, doc.JWKSURI)
	}

	return doc.JWKSURI, nil
}

// hashEntry is one referenced-secret datum contributing to the rollout hash.
// present distinguishes a missing Secret/key (hashed as an absent discriminator,
// see computeConfigHash) from one whose value happens to be empty, so a Secret
// appearing later still changes the hash and rolls the pods.
type hashEntry struct {
	id      string
	present bool
	value   []byte
}

// referencedSecretData reads the Secret keys the API actually consumes and
// returns them as hash entries. It reads only what the current spec references:
// the database Secret always, the TLS Secret when spec.api.tls is set, and the
// JWKS Secret when spec.api.auth.jwkCertSecretRef is set. A missing Secret is not
// an error here (see Reconcile); its keys are recorded as absent.
func (r *HyperFleetConfigReconciler) referencedSecretData(ctx context.Context, cr *hyperfleetv1alpha1.HyperFleetConfig) ([]hashEntry, error) {
	// secretCache avoids re-reading the same Secret if two roles ever point at it.
	secretCache := map[string]*corev1.Secret{}

	// getSecret returns (secret, found, err). NotFound is reported as found=false
	// with no error; any other error is returned.
	getSecret := func(name string) (*corev1.Secret, bool, error) {
		if s, ok := secretCache[name]; ok {
			return s, s != nil, nil
		}
		s := &corev1.Secret{}
		key := types.NamespacedName{Name: name, Namespace: r.OperatorNamespace}
		if err := r.Get(ctx, key, s); err != nil {
			if apierrors.IsNotFound(err) {
				secretCache[name] = nil
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("get secret %q: %w", name, err)
		}
		secretCache[name] = s
		return s, true, nil
	}

	var entries []hashEntry

	// appendKeys records one entry per key from the named Secret under a role
	// prefix (e.g. "database/db.host"), marking each absent if the Secret or key
	// is missing.
	appendKeys := func(role, secretName string, keys []string) error {
		s, found, err := getSecret(secretName)
		if err != nil {
			return err
		}
		for _, k := range keys {
			e := hashEntry{id: role + "/" + k}
			if found {
				if v, ok := s.Data[k]; ok {
					e.present = true
					e.value = v
				}
			}
			entries = append(entries, e)
		}
		return nil
	}

	if err := appendKeys("database", cr.Spec.API.Database.SecretRef.Name, []string{
		apicomponent.SecretKeyDBHost,
		apicomponent.SecretKeyDBPort,
		apicomponent.SecretKeyDBName,
		apicomponent.SecretKeyDBUser,
		apicomponent.SecretKeyDBPassword,
	}); err != nil {
		return nil, err
	}

	if cr.Spec.API.TLS != nil {
		if err := appendKeys("tls", cr.Spec.API.TLS.SecretRef.Name, []string{
			apicomponent.SecretKeyTLSCert,
			apicomponent.SecretKeyTLSKey,
		}); err != nil {
			return nil, err
		}
	}

	if apicomponent.AuthEnabled(cr) && cr.Spec.API.Auth.JWKCertSecretRef != nil {
		if err := appendKeys("jwks", cr.Spec.API.Auth.JWKCertSecretRef.Name, []string{
			apicomponent.SecretKeyJWKS,
		}); err != nil {
			return nil, err
		}
	}

	return entries, nil
}

// computeConfigHash returns a stable SHA-256 over the rendered config.yaml and
// the referenced-secret entries. Entries are sorted by id and every field is
// length-delimited with a NUL separator so distinct inputs cannot collide by
// concatenation (e.g. "ab"+"c" vs "a"+"bc").
func computeConfigHash(configYAML string, entries []hashEntry) string {
	sorted := make([]hashEntry, len(entries))
	copy(sorted, entries)
	slices.SortFunc(sorted, func(a, b hashEntry) int { return strings.Compare(a.id, b.id) })

	h := sha256.New()
	writeField := func(b []byte) {
		// Length-prefix each field so boundaries are unambiguous.
		_, _ = io.WriteString(h, fmt.Sprintf("%d:", len(b)))
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{0})
	}

	writeField([]byte(configYAML))
	for _, e := range sorted {
		writeField([]byte(e.id))
		// A one-byte present/absent discriminator precedes the value so a missing
		// datum can never collide with a present value that happens to equal the
		// absent marker's bytes. Absent writes only the 0x00 tag; present writes
		// 0x01 followed by the length-delimited value.
		if e.present {
			_, _ = h.Write([]byte{1})
			writeField(e.value)
		} else {
			_, _ = h.Write([]byte{0})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// stampConfigHash computes the rollout hash from the component's rendered
// ConfigMap plus the referenced-secret entries and writes it onto the component's
// Deployment pod-template annotations. It matches operands by the API component's
// well-known names, so for a component without that ConfigMap+Deployment pair it
// is a no-op. Mutating the Deployment in place is safe: the object was just
// rendered and has not yet been applied.
func stampConfigHash(objs []client.Object, entries []hashEntry) {
	var configYAML string
	for _, o := range objs {
		if cm, ok := o.(*corev1.ConfigMap); ok && cm.Name == apicomponent.ConfigMapName {
			configYAML = cm.Data[apicomponent.ConfigFileKey]
			break
		}
	}

	hash := computeConfigHash(configYAML, entries)

	for _, o := range objs {
		dep, ok := o.(*appsv1.Deployment)
		if !ok || dep.Name != apicomponent.ResourceName {
			continue
		}
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = map[string]string{}
		}
		dep.Spec.Template.Annotations[configHashAnnotation] = hash
	}
}
