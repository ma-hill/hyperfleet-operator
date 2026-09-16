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

package apply

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
)

// TestObjectsRefreshesRenderedObjects verifies the contract internal/bundle
// relies on: after apply, the same typed object instance is refreshed with the
// live state from the API server, including status written by another
// controller.
func TestObjectsRefreshesRenderedObjects(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(hyperfleetv1alpha1.AddToScheme(scheme)).To(Succeed())
	g.Expect(appsv1.AddToScheme(scheme)).To(Succeed())

	owner := &hyperfleetv1alpha1.HyperFleetConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: hyperfleetv1alpha1.GroupVersion.String(),
			Kind:       "HyperFleetConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: hyperfleetv1alpha1.SingletonName,
		},
	}

	live := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api",
			Namespace: "hyperfleet-system",
		},
		Status: appsv1.DeploymentStatus{
			Replicas:           1,
			AvailableReplicas:  1,
			ReadyReplicas:      1,
			UpdatedReplicas:    1,
			ObservedGeneration: 7,
			Conditions: []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&appsv1.Deployment{}).
		WithObjects(live).
		Build()

	rendered := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      live.Name,
			Namespace: live.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "api", Image: "example.com/api:latest"},
					},
				},
			},
		},
	}

	err := Objects(context.Background(), c, nil, owner, scheme, []client.Object{rendered})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(rendered.Status.AvailableReplicas).To(Equal(live.Status.AvailableReplicas))
	g.Expect(rendered.Status.UpdatedReplicas).To(Equal(live.Status.UpdatedReplicas))
	g.Expect(rendered.Status.ObservedGeneration).To(Equal(live.Status.ObservedGeneration))
	g.Expect(rendered.OwnerReferences).NotTo(BeEmpty())
}

// TestObjectsRefreshUsesProvidedReader verifies the post-apply refresh can read
// from a different source than the writer, which is how the reconciler bypasses
// the manager cache with GetAPIReader.
func TestObjectsRefreshUsesProvidedReader(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(hyperfleetv1alpha1.AddToScheme(scheme)).To(Succeed())
	g.Expect(appsv1.AddToScheme(scheme)).To(Succeed())

	owner := &hyperfleetv1alpha1.HyperFleetConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: hyperfleetv1alpha1.GroupVersion.String(),
			Kind:       "HyperFleetConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: hyperfleetv1alpha1.SingletonName,
		},
	}

	writerObj := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api",
			Namespace: "hyperfleet-system",
		},
	}

	readerObj := writerObj.DeepCopy()
	readerObj.Status = appsv1.DeploymentStatus{
		Replicas:           3,
		AvailableReplicas:  2,
		ReadyReplicas:      2,
		UpdatedReplicas:    2,
		ObservedGeneration: 11,
	}

	writer := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&appsv1.Deployment{}).
		WithObjects(writerObj.DeepCopy()).
		Build()
	reader := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(readerObj.DeepCopy()).
		Build()

	rendered := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      writerObj.Name,
			Namespace: writerObj.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "api", Image: "example.com/api:latest"},
					},
				},
			},
		},
	}

	err := Objects(context.Background(), writer, reader, owner, scheme, []client.Object{rendered})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(rendered.Status.AvailableReplicas).To(Equal(readerObj.Status.AvailableReplicas))
	g.Expect(rendered.Status.UpdatedReplicas).To(Equal(readerObj.Status.UpdatedReplicas))
	g.Expect(rendered.Status.ObservedGeneration).To(Equal(readerObj.Status.ObservedGeneration))

	written := &appsv1.Deployment{}
	written.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind("Deployment"))
	err = writer.Get(context.Background(), client.ObjectKeyFromObject(writerObj), written)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(written.OwnerReferences).NotTo(BeEmpty())
	g.Expect(written.Spec.Template.Spec.Containers).To(HaveLen(1))
	g.Expect(written.Spec.Template.Spec.Containers[0].Image).To(Equal("example.com/api:latest"))
}
