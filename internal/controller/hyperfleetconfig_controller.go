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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
	"github.com/openshift-hyperfleet/hyperfleet-operator/internal/apply"
	"github.com/openshift-hyperfleet/hyperfleet-operator/internal/bundle"
)

// HyperFleetConfigReconciler reconciles a HyperFleetConfig object. It is the
// single bundle controller: it resolves spec.bundle to a component set and
// reconciles each component's operands via server-side apply.
type HyperFleetConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// OperatorNamespace is the namespace the operator runs in and where all
	// operands are created. Sourced from POD_NAMESPACE (downward API) in main.go.
	OperatorNamespace string
	// APIImage is the container image for the API operand. Sourced from
	// RELATED_IMAGE_HYPERFLEET_API in main.go; empty falls back to the API
	// component's compiled-in default.
	APIImage string
}

// +kubebuilder:rbac:groups=hyperfleet.redhat.com,resources=hyperfleetconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hyperfleet.redhat.com,resources=hyperfleetconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hyperfleet.redhat.com,resources=hyperfleetconfigs/finalizers,verbs=update
// The operator reconciles operands with server-side apply (create/update/patch)
// and relies on owner-reference garbage collection (run by kube-controller-manager,
// not this operator) for cleanup, so it needs no delete permission on operands.
// get;list;watch back the Owns() informer caches.
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services;serviceaccounts;configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch

// Reconcile drives the cluster toward the desired state for the HyperFleetConfig
// singleton. It is level-based and idempotent: it renders each component's
// desired operands from the CR and server-side-applies them, so running it twice
// with no spec change writes nothing, and out-of-band drift self-heals (the
// Owns() watches re-invoke this loop).
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *HyperFleetConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cr := &hyperfleetv1alpha1.HyperFleetConfig{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// The CR is gone: its operands carry controller owner references, so the
		// built-in garbage collector removes them. No finalizer is required.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	components := bundle.Resolve(cr.Spec.Bundle, bundle.Config{
		APIImage:  r.APIImage,
		Namespace: r.OperatorNamespace,
	})

	for _, component := range components {
		objs, err := component.Render(ctx, cr)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("render component %q: %w", component.Name(), err)
		}
		if err := apply.Objects(ctx, r.Client, cr, r.Scheme, objs); err != nil {
			return ctrl.Result{}, fmt.Errorf("apply component %q: %w", component.Name(), err)
		}
	}

	// TODO(HYPERFLEET-1408): map CR fields (database/auth/tls/profile) into the
	// rendered operand — env, config-file content, secret mounts, resources and
	// replicas — plus a content-hash annotation to roll pods on config change.
	// TODO(HYPERFLEET-1409): roll each component's Conditions up into
	// status.conditions and set status.observedGeneration.
	// TODO(HYPERFLEET-1512): resolve spec.api.database.secretRef (and any
	// tls.secretRef) in r.OperatorNamespace and surface a Degraded condition when
	// the referenced Secret is missing.

	log.Info("reconciled HyperFleetConfig",
		"bundle", cr.Spec.Bundle, "components", len(components), "namespace", r.OperatorNamespace)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager. It watches the
// HyperFleetConfig and every operand type it owns, so an out-of-band change to
// any operand re-invokes Reconcile and the desired state is re-applied.
func (r *HyperFleetConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hyperfleetv1alpha1.HyperFleetConfig{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Named("hyperfleetconfig").
		Complete(r)
}
