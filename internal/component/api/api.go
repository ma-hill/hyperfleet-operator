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
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
)

// DefaultImage is the compiled-in fallback image used when the operator is not
// given RELATED_IMAGE_HYPERFLEET_API. Production deployments override it with a
// digest-pinned image via that env var (OLM relatedImages convention).
const DefaultImage = "quay.io/openshift-hyperfleet/hyperfleet-api:latest"

// Component renders the HyperFleet API operand. It satisfies the bundle.Component
// contract structurally (no import of internal/bundle, avoiding an import cycle:
// bundle imports this package). It carries only rendering inputs — the Image and
// the target Namespace — and never touches the cluster; the controller applies
// what Render produces.
type Component struct {
	// Image is the API container image. Empty falls back to DefaultImage.
	Image string
	// Namespace is the operator's own namespace, where all operands live.
	Namespace string
}

// New constructs the API component for the given image and operator namespace.
func New(image, namespace string) *Component {
	return &Component{Image: image, Namespace: namespace}
}

// Name identifies the component in logs and the app.kubernetes.io/component label.
func (c *Component) Name() string {
	return ComponentName
}

// Render returns the full desired-state operand set for the API, in a
// dependency-friendly order (identity and RBAC before the workload that uses
// them). It is a pure function of its inputs: no cluster reads or writes.
//
// The returned objects are structurally complete but not yet configured from the
// CR spec — database/auth/tls env, config-file content and profile→resources are
// wired in HYPERFLEET-1408. The ctx is part of the contract (later components may
// need it) but is unused here.
func (c *Component) Render(_ context.Context, cr *hyperfleetv1alpha1.HyperFleetConfig) ([]client.Object, error) {
	image := c.Image
	if image == "" {
		image = DefaultImage
	}

	return []client.Object{
		serviceAccount(cr, c.Namespace),
		role(cr, c.Namespace),
		roleBinding(cr, c.Namespace),
		configMap(cr, c.Namespace),
		service(cr, c.Namespace),
		deployment(cr, image, c.Namespace),
	}, nil
}

// Conditions reports the component's health as metav1.Conditions. The contract is
// defined now (HYPERFLEET-1407) so it need not be reopened next story, but its
// output is not yet rolled up into status.conditions — real health derivation and
// status wiring land in HYPERFLEET-1409. Returning nil until then keeps the
// reconciler from writing status prematurely.
func (c *Component) Conditions(_ context.Context, _ *hyperfleetv1alpha1.HyperFleetConfig) ([]metav1.Condition, error) {
	return nil, nil
}
