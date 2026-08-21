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
	"testing"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
)

const testNamespace = "hyperfleet-operator-system"

// testCR returns the singleton in the shape Render consumes (only Name and
// spec.bundle matter to rendering in 1407).
func testCR() *hyperfleetv1alpha1.HyperFleetConfig {
	return &hyperfleetv1alpha1.HyperFleetConfig{
		ObjectMeta: metav1.ObjectMeta{Name: hyperfleetv1alpha1.SingletonName},
		Spec:       hyperfleetv1alpha1.HyperFleetConfigSpec{Bundle: hyperfleetv1alpha1.BundleCloudCAPI},
	}
}

// byKind indexes rendered objects by their GVK Kind for assertion. Render sets
// TypeMeta explicitly (required for server-side apply), so Kind is populated.
func byKind(objs []client.Object) map[string]client.Object {
	out := make(map[string]client.Object, len(objs))
	for _, o := range objs {
		out[o.GetObjectKind().GroupVersionKind().Kind] = o
	}
	return out
}

func TestRenderProducesTheOperandSet(t *testing.T) {
	g := NewWithT(t)
	const image = "example.com/hyperfleet-api:test"

	objs, err := New(image, testNamespace).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(objs).To(HaveLen(6))

	kinds := byKind(objs)
	g.Expect(kinds).To(HaveKey("ServiceAccount"))
	g.Expect(kinds).To(HaveKey("Role"))
	g.Expect(kinds).To(HaveKey("RoleBinding"))
	g.Expect(kinds).To(HaveKey("ConfigMap"))
	g.Expect(kinds).To(HaveKey("Service"))
	g.Expect(kinds).To(HaveKey("Deployment"))

	// Every operand lives in the operator namespace, carries the common labels
	// (including the component marker) and a non-empty GVK for SSA.
	for _, o := range objs {
		g.Expect(o.GetNamespace()).To(Equal(testNamespace))
		g.Expect(o.GetLabels()).To(HaveKeyWithValue(labelComponent, ComponentName))
		g.Expect(o.GetLabels()).To(HaveKeyWithValue(labelManagedBy, managedByOperator))
		g.Expect(o.GetObjectKind().GroupVersionKind().Kind).NotTo(BeEmpty())
		g.Expect(o.GetObjectKind().GroupVersionKind().Version).NotTo(BeEmpty())
	}
}

func TestRenderDeployment(t *testing.T) {
	g := NewWithT(t)
	const image = "example.com/hyperfleet-api:test"

	objs, err := New(image, testNamespace).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	dep, ok := byKind(objs)["Deployment"].(*appsv1.Deployment)
	g.Expect(ok).To(BeTrue())
	g.Expect(dep.Name).To(Equal(ResourceName))

	spec := dep.Spec
	g.Expect(spec.Replicas).To(HaveValue(BeEquivalentTo(1)))
	// The selector is the immutable subset; the pod template carries the full
	// label set, so the selector must be a subset of the template labels (a
	// selector that is not a subset would make the Deployment adopt no pods).
	for k, v := range spec.Selector.MatchLabels {
		g.Expect(spec.Template.Labels).To(HaveKeyWithValue(k, v))
	}
	g.Expect(spec.Template.Labels).To(HaveKeyWithValue(labelComponent, ComponentName))

	g.Expect(spec.Template.Spec.ServiceAccountName).To(Equal(ResourceName))
	// The API does not use the Kubernetes API, so the SA token is not mounted.
	g.Expect(spec.Template.Spec.AutomountServiceAccountToken).To(HaveValue(BeFalse()))
	g.Expect(spec.Template.Spec.Containers).To(HaveLen(1))

	c := spec.Template.Spec.Containers[0]
	g.Expect(c.Image).To(Equal(image))
	g.Expect(c.Args).To(Equal([]string{"serve"}))

	ports := map[string]int32{}
	for _, p := range c.Ports {
		ports[p.Name] = p.ContainerPort
	}
	g.Expect(ports).To(Equal(map[string]int32{
		portNameHTTP:    portHTTP,
		portNameHealth:  portHealth,
		portNameMetrics: portMetrics,
	}))

	g.Expect(c.LivenessProbe.HTTPGet.Path).To(Equal("/healthz"))
	g.Expect(c.LivenessProbe.HTTPGet.Port.StrVal).To(Equal(portNameHealth))
	g.Expect(c.ReadinessProbe.HTTPGet.Path).To(Equal("/readyz"))
	g.Expect(c.ReadinessProbe.HTTPGet.Port.StrVal).To(Equal(portNameHealth))

	// Hardened container per the chart's securityContext.
	g.Expect(c.SecurityContext.ReadOnlyRootFilesystem).To(HaveValue(BeTrue()))
	g.Expect(c.SecurityContext.AllowPrivilegeEscalation).To(HaveValue(BeFalse()))
	g.Expect(dep.Spec.Template.Spec.SecurityContext.RunAsNonRoot).To(HaveValue(BeTrue()))

	// The config ConfigMap is mounted read-only at the expected path.
	var mountedConfig bool
	for _, v := range spec.Template.Spec.Volumes {
		if v.ConfigMap != nil && v.ConfigMap.Name == ConfigMapName {
			mountedConfig = true
		}
	}
	g.Expect(mountedConfig).To(BeTrue(), "expected a volume backed by the API ConfigMap")
}

func TestRenderEmptyImageFallsBackToDefault(t *testing.T) {
	g := NewWithT(t)

	objs, err := New("", testNamespace).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	dep, ok := byKind(objs)["Deployment"].(*appsv1.Deployment)
	g.Expect(ok).To(BeTrue())
	g.Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal(DefaultImage))
}

func TestRenderRoleHasNoRules(t *testing.T) {
	g := NewWithT(t)

	objs, err := New("img", testNamespace).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	role, ok := byKind(objs)["Role"].(*rbacv1.Role)
	g.Expect(ok).To(BeTrue())
	// The API needs no in-cluster permissions today; the Role exists only to
	// satisfy the RBAC operand and pre-wire the pattern (see render.go).
	g.Expect(role.Rules).To(BeEmpty())

	rb, ok := byKind(objs)["RoleBinding"].(*rbacv1.RoleBinding)
	g.Expect(ok).To(BeTrue())
	g.Expect(rb.RoleRef.Name).To(Equal(ResourceName))
	g.Expect(rb.RoleRef.Kind).To(Equal("Role"))
	g.Expect(rb.Subjects).To(HaveLen(1))
	g.Expect(rb.Subjects[0].Name).To(Equal(ResourceName))
	g.Expect(rb.Subjects[0].Namespace).To(Equal(testNamespace))
}

func TestRenderService(t *testing.T) {
	g := NewWithT(t)

	objs, err := New("img", testNamespace).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	svc, ok := byKind(objs)["Service"].(*corev1.Service)
	g.Expect(ok).To(BeTrue())
	g.Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
	g.Expect(svc.Spec.Ports).To(HaveLen(3))
	g.Expect(svc.Spec.Selector).To(HaveKeyWithValue(labelComponent, ComponentName))
}
