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

// Package bundle defines the component contract and resolves a bundle to its
// ordered component set. It is the in-operator "bundle definition": adding a
// component later is one new entry here plus its own package — never a new
// controller.
package bundle

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
	"github.com/openshift-hyperfleet/hyperfleet-operator/internal/component/api"
)

// Component is the contract every component satisfies. It is deliberately tiny —
// render the desired objects, report health — with no extension points until a
// second component exists to justify them.
//
//   - Render is a pure function (CR → desired objects); it must not read or write
//     the cluster. The controller applies what it returns.
//   - Conditions reports component health. It is consumed starting in
//     HYPERFLEET-1409; until then the controller does not roll it into status.
type Component interface {
	Name() string
	Render(ctx context.Context, cr *hyperfleetv1alpha1.HyperFleetConfig) ([]client.Object, error)
	Conditions(ctx context.Context, cr *hyperfleetv1alpha1.HyperFleetConfig) ([]metav1.Condition, error)
}

// Config carries the inputs the resolver needs to construct components.
type Config struct {
	// APIImage is the image for the API component (empty → its compiled-in default).
	APIImage string
	// Namespace is the operator's own namespace, where operands are created.
	Namespace string
}

// sharedTier lists the components present in every bundle regardless of flavor.
// In phase 1 this is exactly [API], so every bundle resolves to [API].
func sharedTier(cfg Config) []Component {
	return []Component{
		api.New(cfg.APIImage, cfg.Namespace),
	}
}

// bundleSpecific returns the components unique to a bundle beyond the shared
// tier. Phase 1 has none for either bundle; this is the single extension point
// where a future bundle-specific component is registered.
func bundleSpecific(_ hyperfleetv1alpha1.BundleType) []Component {
	return nil
}

// Resolve maps a bundle to its ordered component set: the shared tier followed by
// any bundle-specific components. The shared tier is first so its components
// (currently the API) reconcile before anything that might depend on them.
func Resolve(b hyperfleetv1alpha1.BundleType, cfg Config) []Component {
	return append(sharedTier(cfg), bundleSpecific(b)...)
}
