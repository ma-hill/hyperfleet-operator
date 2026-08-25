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

// Package apply centralizes the idempotent server-side-apply upsert used to
// reconcile operands: stamp a controller owner reference, then apply.
package apply

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
)

// FieldManager is the server-side-apply field owner stamped on every operand the
// operator manages. It lets the API server distinguish operator-owned fields from
// any a human sets, which is what makes ForceOwnership drift-correction safe: the
// operator is the sole intended manager of these fields.
const FieldManager = "hyperfleet-operator"

// Objects upserts each desired operand via server-side apply. For every object it:
//
//  1. sets a controller owner reference back to the CR, so the object is
//     garbage-collected with the CR and wakes the controller via its Owns() watch
//     when it drifts (a cluster-scoped owner owning namespaced dependents is
//     valid — the owner has no namespace);
//  2. applies the object with a fixed field manager and ForceOwnership, so
//     re-applying identical state is a no-op (idempotent) while out-of-band edits
//     to operator-owned fields are reclaimed.
//
// Each object must carry its GVK (TypeMeta) — server-side apply requires it.
// Errors are wrapped with the object's kind and name so the caller can see which
// operand failed.
func Objects(
	ctx context.Context,
	c client.Client,
	owner *hyperfleetv1alpha1.HyperFleetConfig,
	scheme *runtime.Scheme,
	objs []client.Object,
) error {
	for _, obj := range objs {
		kind := obj.GetObjectKind().GroupVersionKind().Kind
		if err := ctrl.SetControllerReference(owner, obj, scheme); err != nil {
			return fmt.Errorf("set controller reference on %s %q: %w", kind, obj.GetName(), err)
		}
		if err := c.Patch(ctx, obj, client.Apply, client.FieldOwner(FieldManager), client.ForceOwnership); err != nil {
			return fmt.Errorf("apply %s %q: %w", kind, obj.GetName(), err)
		}
	}
	return nil
}
