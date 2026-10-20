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

package sentinel

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
	"github.com/openshift-hyperfleet/hyperfleet-operator/internal/component"
)

const (
	testNamespace    = "hyperfleet-system"
	testImage        = "example.com/hyperfleet-sentinel:test"
	testResourceType = "clusters"
)

func testComponent() *Component {
	return New(Options{
		Image:        testImage,
		Namespace:    testNamespace,
		ResourceType: testResourceType,
		APIBaseURL:   "http://hyperfleet-api:8000",
		BrokerType:   BrokerTypeRabbitMQ,
	})
}

func testCR() *hyperfleetv1alpha1.HyperFleetConfig {
	return &hyperfleetv1alpha1.HyperFleetConfig{
		ObjectMeta: metav1.ObjectMeta{Name: hyperfleetv1alpha1.SingletonName},
		Spec: hyperfleetv1alpha1.HyperFleetConfigSpec{
			Bundle: hyperfleetv1alpha1.BundleCloudCAPI,
		},
	}
}

func byKindName(objs []client.Object) map[string]client.Object {
	out := make(map[string]client.Object, len(objs))
	for _, o := range objs {
		key := o.GetObjectKind().GroupVersionKind().Kind + "/" + o.GetName()
		out[key] = o
	}
	return out
}

func deploymentWithStatus(specReplicas, statusReplicas, availableReplicas, updatedReplicas int32, generation int64) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: ResourceName(testResourceType), Generation: generation},
		Spec:       appsv1.DeploymentSpec{Replicas: ptr.To(specReplicas)},
		Status: appsv1.DeploymentStatus{
			Replicas:           statusReplicas,
			AvailableReplicas:  availableReplicas,
			UpdatedReplicas:    updatedReplicas,
			ObservedGeneration: generation,
		},
	}
}

func condMapByType(conds []metav1.Condition) map[string]metav1.Condition {
	out := make(map[string]metav1.Condition, len(conds))
	for _, c := range conds {
		out[c.Type] = c
	}
	return out
}

func TestName(t *testing.T) {
	g := NewWithT(t)
	g.Expect(New(Options{ResourceType: "clusters"}).Name()).To(Equal("sentinel-clusters"))
}

func TestRenderProducesTheOperandSet(t *testing.T) {
	g := NewWithT(t)

	objs, err := testComponent().Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(objs).To(HaveLen(5))

	kinds := byKindName(objs)
	g.Expect(kinds).To(HaveKey("ServiceAccount/" + ResourceName(testResourceType)))
	g.Expect(kinds).To(HaveKey("ConfigMap/" + ConfigMapName(testResourceType)))
	g.Expect(kinds).To(HaveKey("ConfigMap/" + BrokerConfigMapName(testResourceType)))
	g.Expect(kinds).To(HaveKey("NetworkPolicy/" + ResourceName(testResourceType)))
	g.Expect(kinds).To(HaveKey("Deployment/" + ResourceName(testResourceType)))

	expectedComponent := componentName(testResourceType)
	for _, o := range objs {
		g.Expect(o.GetNamespace()).To(Equal(testNamespace))
		g.Expect(o.GetLabels()).To(HaveKeyWithValue(labelComponent, expectedComponent))
		g.Expect(o.GetLabels()).To(HaveKeyWithValue(labelManagedBy, managedByOperator))
		g.Expect(o.GetObjectKind().GroupVersionKind().Kind).NotTo(BeEmpty())
		g.Expect(o.GetObjectKind().GroupVersionKind().Version).NotTo(BeEmpty())
	}
}

func TestRenderDeployment(t *testing.T) {
	g := NewWithT(t)

	objs, err := testComponent().Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	dep, ok := byKindName(objs)["Deployment/"+ResourceName(testResourceType)].(*appsv1.Deployment)
	g.Expect(ok).To(BeTrue())
	g.Expect(dep.Name).To(Equal(ResourceName(testResourceType)))

	spec := dep.Spec
	g.Expect(spec.Replicas).To(HaveValue(BeEquivalentTo(1)))
	for k, v := range spec.Selector.MatchLabels {
		g.Expect(spec.Template.Labels).To(HaveKeyWithValue(k, v))
	}

	g.Expect(spec.Template.Spec.ServiceAccountName).To(Equal(ResourceName(testResourceType)))
	g.Expect(spec.Template.Spec.AutomountServiceAccountToken).To(HaveValue(BeFalse()))
	g.Expect(spec.Template.Spec.Containers).To(HaveLen(1))

	c := spec.Template.Spec.Containers[0]
	g.Expect(c.Image).To(Equal(testImage))
	g.Expect(c.Args).To(Equal([]string{"serve", "--config=" + configMountPath}))

	ports := map[string]int32{}
	for _, p := range c.Ports {
		ports[p.Name] = p.ContainerPort
	}
	g.Expect(ports).To(Equal(map[string]int32{
		portNameHTTP:    portHTTP,
		portNameMetrics: portMetrics,
	}))

	g.Expect(c.LivenessProbe.HTTPGet.Path).To(Equal("/healthz"))
	g.Expect(c.LivenessProbe.HTTPGet.Port.StrVal).To(Equal(portNameHTTP))
	g.Expect(c.ReadinessProbe.HTTPGet.Path).To(Equal("/readyz"))
	g.Expect(c.ReadinessProbe.HTTPGet.Port.StrVal).To(Equal(portNameHTTP))

	g.Expect(c.SecurityContext.ReadOnlyRootFilesystem).To(HaveValue(BeTrue()))
	g.Expect(c.SecurityContext.AllowPrivilegeEscalation).To(HaveValue(BeFalse()))
	g.Expect(dep.Spec.Template.Spec.SecurityContext.RunAsNonRoot).To(HaveValue(BeTrue()))
	g.Expect(dep.Spec.Template.Spec.SecurityContext.RunAsUser).To(BeNil())
	g.Expect(dep.Spec.Template.Spec.SecurityContext.FSGroup).To(BeNil())

	var mountedConfig, mountedBrokerConfig bool
	for _, v := range spec.Template.Spec.Volumes {
		if v.ConfigMap != nil && v.ConfigMap.Name == ConfigMapName(testResourceType) {
			mountedConfig = true
		}
		if v.ConfigMap != nil && v.ConfigMap.Name == BrokerConfigMapName(testResourceType) {
			mountedBrokerConfig = true
		}
	}
	g.Expect(mountedConfig).To(BeTrue(), "expected a volume backed by the config ConfigMap")
	g.Expect(mountedBrokerConfig).To(BeTrue(), "expected a volume backed by the broker-config ConfigMap")
}

func TestRenderDeploymentRabbitMQEnv(t *testing.T) {
	g := NewWithT(t)

	c := testComponent()
	objs, err := c.Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	dep, ok := byKindName(objs)["Deployment/"+ResourceName(testResourceType)].(*appsv1.Deployment)
	g.Expect(ok).To(BeTrue())

	envMap := map[string]corev1.EnvVar{}
	for _, e := range dep.Spec.Template.Spec.Containers[0].Env {
		envMap[e.Name] = e
	}

	g.Expect(envMap).To(HaveKey(brokerConfigEnv))
	g.Expect(envMap[brokerConfigEnv].Value).To(Equal(brokerConfigMountPath))

	g.Expect(envMap).To(HaveKey(brokerSecretEnv))
	g.Expect(envMap[brokerSecretEnv].ValueFrom).NotTo(BeNil())
	g.Expect(envMap[brokerSecretEnv].ValueFrom.SecretKeyRef.Name).To(Equal(BrokerSecretName))
	g.Expect(envMap[brokerSecretEnv].ValueFrom.SecretKeyRef.Key).To(Equal(BrokerSecretKey))
}

func TestRenderDeploymentGooglePubSubNoSecretEnv(t *testing.T) {
	g := NewWithT(t)

	t.Setenv(BrokerGooglePubSubProjectIDEnv, "my-project")

	c := New(Options{
		Image:        testImage,
		Namespace:    testNamespace,
		ResourceType: testResourceType,
		APIBaseURL:   "http://hyperfleet-api:8000",
		BrokerType:   BrokerTypeGooglePubSub,
	})

	objs, err := c.Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	dep, ok := byKindName(objs)["Deployment/"+ResourceName(testResourceType)].(*appsv1.Deployment)
	g.Expect(ok).To(BeTrue())

	envNames := make([]string, 0, len(dep.Spec.Template.Spec.Containers[0].Env))
	for _, e := range dep.Spec.Template.Spec.Containers[0].Env {
		envNames = append(envNames, e.Name)
	}

	g.Expect(envNames).To(ContainElement(brokerConfigEnv))
	g.Expect(envNames).NotTo(ContainElement(brokerSecretEnv),
		"Google Pub/Sub should not inject the RabbitMQ secret env var")
}

func TestRenderEmptyImageErrors(t *testing.T) {
	g := NewWithT(t)

	c := New(Options{
		Namespace:    testNamespace,
		ResourceType: testResourceType,
		BrokerType:   BrokerTypeRabbitMQ,
	})
	_, err := c.Render(context.Background(), testCR())
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("sentinel image is required"))
}

func TestRenderNetworkPolicy(t *testing.T) {
	g := NewWithT(t)

	objs, err := testComponent().Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	np, ok := byKindName(objs)["NetworkPolicy/"+ResourceName(testResourceType)].(*networkingv1.NetworkPolicy)
	g.Expect(ok).To(BeTrue())

	g.Expect(np.Spec.PolicyTypes).To(ConsistOf(networkingv1.PolicyTypeIngress))
	g.Expect(np.Spec.Ingress).To(BeEmpty(), "ingress should be deny-all (empty rule list)")
	g.Expect(np.Spec.PodSelector.MatchLabels).To(Equal(selectorLabels(hyperfleetv1alpha1.SingletonName, testResourceType)))
}

func TestRolloutReturnsTwoConfigEntries(t *testing.T) {
	g := NewWithT(t)

	c := testComponent()
	target := c.Rollout()

	g.Expect(target.ConfigEntries).To(HaveLen(2))
	g.Expect(target.ConfigEntries).To(Equal([]component.ConfigEntry{
		{ConfigMapName: ConfigMapName(testResourceType), ConfigFileKey: ConfigFileKey},
		{ConfigMapName: BrokerConfigMapName(testResourceType), ConfigFileKey: BrokerConfigFileKey},
	}))
	g.Expect(target.DeploymentName).To(Equal(ResourceName(testResourceType)))
}

func TestConditionsNoDeployment(t *testing.T) {
	g := NewWithT(t)

	conds, err := testComponent().Conditions(context.Background(), testCR(), nil)
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Status).To(Equal(metav1.ConditionFalse))
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Reason).To(Equal(hyperfleetv1alpha1.ReasonDeploymentUnavailable))
	g.Expect(byType[hyperfleetv1alpha1.ConditionProgressing].Status).To(Equal(metav1.ConditionTrue))
	g.Expect(byType[hyperfleetv1alpha1.ConditionProgressing].Reason).To(Equal(hyperfleetv1alpha1.ReasonRolloutInProgress))
}

func TestConditionsZeroAvailableReplicas(t *testing.T) {
	g := NewWithT(t)

	dep := deploymentWithStatus(1, 1, 0, 0, 1)
	conds, err := testComponent().Conditions(context.Background(), testCR(), []client.Object{dep})
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Status).To(Equal(metav1.ConditionFalse))
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Reason).To(Equal(hyperfleetv1alpha1.ReasonDeploymentUnavailable))
}

func TestConditionsPartiallyReady(t *testing.T) {
	g := NewWithT(t)

	dep := deploymentWithStatus(2, 2, 1, 1, 1)
	conds, err := testComponent().Conditions(context.Background(), testCR(), []client.Object{dep})
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Status).To(Equal(metav1.ConditionFalse))
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Reason).To(Equal(hyperfleetv1alpha1.ReasonDeploymentNotReady))
	g.Expect(byType[hyperfleetv1alpha1.ConditionProgressing].Status).To(Equal(metav1.ConditionTrue))
}

func TestConditionsFullyReadyAndStable(t *testing.T) {
	g := NewWithT(t)

	dep := deploymentWithStatus(1, 1, 1, 1, 1)
	conds, err := testComponent().Conditions(context.Background(), testCR(), []client.Object{dep})
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Status).To(Equal(metav1.ConditionTrue))
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Reason).To(Equal(hyperfleetv1alpha1.ReasonDeploymentAvailable))
	g.Expect(byType[hyperfleetv1alpha1.ConditionProgressing].Status).To(Equal(metav1.ConditionFalse))
	g.Expect(byType[hyperfleetv1alpha1.ConditionProgressing].Reason).To(Equal(hyperfleetv1alpha1.ReasonRolloutComplete))
}

func TestConditionsMidRollout(t *testing.T) {
	g := NewWithT(t)

	dep := deploymentWithStatus(1, 1, 1, 1, 2)
	dep.Status.ObservedGeneration = 1
	conds, err := testComponent().Conditions(context.Background(), testCR(), []client.Object{dep})
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionProgressing].Status).To(Equal(metav1.ConditionTrue))
	g.Expect(byType[hyperfleetv1alpha1.ConditionProgressing].Reason).To(Equal(hyperfleetv1alpha1.ReasonRolloutInProgress))
}

func TestConditionsScaledToZero(t *testing.T) {
	g := NewWithT(t)

	dep := deploymentWithStatus(0, 0, 0, 0, 1)
	conds, err := testComponent().Conditions(context.Background(), testCR(), []client.Object{dep})
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Status).To(Equal(metav1.ConditionTrue))
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Reason).To(Equal(hyperfleetv1alpha1.ReasonDeploymentAvailable))
}

func TestConditionsFindsDeploymentInMixedKindSlice(t *testing.T) {
	g := NewWithT(t)

	dep := deploymentWithStatus(1, 1, 1, 1, 1)
	applied := []client.Object{
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: ResourceName(testResourceType)}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName(testResourceType)}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: BrokerConfigMapName(testResourceType)}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: ResourceName(testResourceType)}},
		dep,
	}

	conds, err := testComponent().Conditions(context.Background(), testCR(), applied)
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Status).To(Equal(metav1.ConditionTrue))
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Reason).To(Equal(hyperfleetv1alpha1.ReasonDeploymentAvailable))
}

func TestConditionsNilSpecReplicasDefaultsToOne(t *testing.T) {
	g := NewWithT(t)

	dep := deploymentWithStatus(1, 1, 1, 1, 1)
	dep.Spec.Replicas = nil

	conds, err := testComponent().Conditions(context.Background(), testCR(), []client.Object{dep})
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Status).To(Equal(metav1.ConditionTrue))
	g.Expect(byType[hyperfleetv1alpha1.ConditionProgressing].Status).To(Equal(metav1.ConditionFalse))
}
