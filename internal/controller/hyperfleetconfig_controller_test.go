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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
)

var _ = Describe("HyperFleetConfig Controller", func() {
	Context("When reconciling a resource", func() {
		// HyperFleetConfig is a cluster-scoped singleton: the only permitted name
		// is "cluster" and there is no namespace.
		const resourceName = hyperfleetv1alpha1.SingletonName

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		hyperfleetconfig := &hyperfleetv1alpha1.HyperFleetConfig{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind HyperFleetConfig")
			err := k8sClient.Get(ctx, typeNamespacedName, hyperfleetconfig)
			if errors.IsNotFound(err) {
				resource := &hyperfleetv1alpha1.HyperFleetConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
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
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			} else {
				// A Get error other than NotFound means the fixture state is unknown;
				// fail loudly instead of silently skipping the Create and letting the
				// reconcile spec pass without its required resource.
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance HyperFleetConfig")
			resource := &hyperfleetv1alpha1.HyperFleetConfig{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
			}
			// NotFound means cleanup already happened; any other delete error
			// must fail the test loudly instead of being masked as an Eventually
			// timeout below.
			if err := k8sClient.Delete(ctx, resource); err != nil && !errors.IsNotFound(err) {
				Expect(err).NotTo(HaveOccurred())
			}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, &hyperfleetv1alpha1.HyperFleetConfig{})
				return errors.IsNotFound(err)
			}).Should(BeTrue())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &HyperFleetConfigReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
