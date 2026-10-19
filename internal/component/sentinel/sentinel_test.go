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
	testResourceType = "clusters"
	testAPIBaseURL   = "http://hyperfleet-api.hyperfleet-system.svc:8000"
)

func testCR() *hyperfleetv1alpha1.HyperFleetConfig {
	return &hyperfleetv1alpha1.HyperFleetConfig{
		ObjectMeta: metav1.ObjectMeta{Name: hyperfleetv1alpha1.SingletonName},
		Spec: hyperfleetv1alpha1.HyperFleetConfigSpec{
			Bundle: hyperfleetv1alpha1.BundleCloudCAPI,
		},
	}
}

func testOptions() Options {
	return Options{ResourceType: testResourceType, APIBaseURL: testAPIBaseURL}
}

// byKind indexes rendered objects by Kind; Render sets TypeMeta explicitly.
func byKind(objs []client.Object) map[string]client.Object {
	out := make(map[string]client.Object, len(objs))
	for _, o := range objs {
		out[o.GetObjectKind().GroupVersionKind().Kind] = o
	}
	return out
}

// findConfigMap returns the named ConfigMap from rendered objects; a resource
// type renders two, so Kind alone cannot select one.
func findConfigMap(objs []client.Object, name string) *corev1.ConfigMap {
	for _, o := range objs {
		if cm, ok := o.(*corev1.ConfigMap); ok && cm.Name == name {
			return cm
		}
	}
	return nil
}

func TestNameIncludesResourceType(t *testing.T) {
	g := NewWithT(t)

	c := New("img", testNamespace, testOptions())
	g.Expect(c.Name()).To(Equal("sentinel-clusters"))

	c2 := New("img", testNamespace, Options{ResourceType: "nodepools"})
	g.Expect(c2.Name()).To(Equal("sentinel-nodepools"))
}

func TestRenderProducesTheOperandSet(t *testing.T) {
	g := NewWithT(t)

	objs, err := New("img", testNamespace, testOptions()).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(objs).To(HaveLen(5))

	kinds := byKind(objs)
	g.Expect(kinds).To(HaveKey("ServiceAccount"))
	g.Expect(kinds).To(HaveKey("NetworkPolicy"))
	g.Expect(kinds).To(HaveKey("Deployment"))
	// Two ConfigMaps (config + broker-config); no Service and no
	// Role/RoleBinding — see render.go's package doc.
	var configMaps int
	for _, o := range objs {
		if _, ok := o.(*corev1.ConfigMap); ok {
			configMaps++
		}
	}
	g.Expect(configMaps).To(Equal(2))
	g.Expect(kinds).NotTo(HaveKey("Service"))
	g.Expect(kinds).NotTo(HaveKey("Role"))
	g.Expect(kinds).NotTo(HaveKey("RoleBinding"))

	for _, o := range objs {
		g.Expect(o.GetNamespace()).To(Equal(testNamespace))
		g.Expect(o.GetLabels()).To(HaveKeyWithValue(labelComponent, "sentinel-clusters"))
		g.Expect(o.GetLabels()).To(HaveKeyWithValue(labelManagedBy, managedByOperator))
		g.Expect(o.GetObjectKind().GroupVersionKind().Kind).NotTo(BeEmpty())
		g.Expect(o.GetObjectKind().GroupVersionKind().Version).NotTo(BeEmpty())
	}
}

func TestRenderNamesAreUniquePerResourceType(t *testing.T) {
	g := NewWithT(t)

	clusterObjs, err := New("img", testNamespace, Options{ResourceType: "clusters"}).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())
	nodepoolObjs, err := New("img", testNamespace, Options{ResourceType: "nodepools"}).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	for _, o := range clusterObjs {
		g.Expect(o.GetName()).To(ContainSubstring("clusters"))
	}
	for _, o := range nodepoolObjs {
		g.Expect(o.GetName()).To(ContainSubstring("nodepools"))
	}
}

func TestRenderDeployment(t *testing.T) {
	g := NewWithT(t)
	const image = "example.com/hyperfleet-sentinel:test"

	objs, err := New(image, testNamespace, testOptions()).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	dep, ok := byKind(objs)["Deployment"].(*appsv1.Deployment)
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
	g.Expect(c.Image).To(Equal(image))
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

	// The broker URL comes from the fixed-name Secret, never a literal.
	var brokerEnv *corev1.EnvVar
	var brokerConfigEnvVar *corev1.EnvVar
	for i, e := range c.Env {
		if e.Name == brokerSecretEnv {
			brokerEnv = &c.Env[i]
		}
		if e.Name == "BROKER_CONFIG_FILE" {
			brokerConfigEnvVar = &c.Env[i]
		}
	}
	g.Expect(brokerEnv).NotTo(BeNil())
	g.Expect(brokerEnv.ValueFrom).NotTo(BeNil())
	g.Expect(brokerEnv.ValueFrom.SecretKeyRef.Name).To(Equal(BrokerSecretName))
	g.Expect(brokerEnv.ValueFrom.SecretKeyRef.Key).To(Equal(BrokerSecretKey))
	g.Expect(brokerConfigEnvVar).NotTo(BeNil())
	g.Expect(brokerConfigEnvVar.Value).To(Equal(brokerConfigMountPath))

	var configVolumeCMs []string
	for _, v := range spec.Template.Spec.Volumes {
		if v.ConfigMap != nil {
			configVolumeCMs = append(configVolumeCMs, v.ConfigMap.Name)
		}
	}
	g.Expect(configVolumeCMs).To(ConsistOf(ConfigMapName(testResourceType), BrokerConfigMapName(testResourceType)))
}

func TestRenderEmptyImageErrors(t *testing.T) {
	g := NewWithT(t)

	_, err := New("", testNamespace, testOptions()).Render(context.Background(), testCR())
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("sentinel image is required"))
}

func TestRenderNetworkPolicyDeniesIngress(t *testing.T) {
	g := NewWithT(t)

	objs, err := New("img", testNamespace, testOptions()).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	np, ok := byKind(objs)["NetworkPolicy"].(*networkingv1.NetworkPolicy)
	g.Expect(ok).To(BeTrue())
	g.Expect(np.Spec.PolicyTypes).To(ConsistOf(networkingv1.PolicyTypeIngress))
	g.Expect(np.Spec.Ingress).To(BeEmpty())
	g.Expect(np.Spec.Egress).To(BeNil())
	// The policy must select this instance's own pods.
	dep := byKind(objs)["Deployment"].(*appsv1.Deployment)
	for k, v := range np.Spec.PodSelector.MatchLabels {
		g.Expect(dep.Spec.Template.Labels).To(HaveKeyWithValue(k, v))
	}
}

func TestRenderConfigMapsSplitConfigAndBroker(t *testing.T) {
	g := NewWithT(t)

	objs, err := New("img", testNamespace, testOptions()).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	cm := findConfigMap(objs, ConfigMapName(testResourceType))
	g.Expect(cm).NotTo(BeNil())
	g.Expect(cm.Data).To(HaveKey(ConfigFileKey))
	g.Expect(cm.Data).NotTo(HaveKey(brokerConfigFileKey))
	g.Expect(cm.Data[ConfigFileKey]).To(ContainSubstring(testAPIBaseURL))
	g.Expect(cm.Data[ConfigFileKey]).To(ContainSubstring("resource_type: " + testResourceType))

	bcm := findConfigMap(objs, BrokerConfigMapName(testResourceType))
	g.Expect(bcm).NotTo(BeNil())
	g.Expect(bcm.Data).To(HaveKey(brokerConfigFileKey))
	g.Expect(bcm.Data).NotTo(HaveKey(ConfigFileKey))
	g.Expect(bcm.Data[brokerConfigFileKey]).To(ContainSubstring("type: rabbitmq"))
}

func TestRenderIsDeterministic(t *testing.T) {
	g := NewWithT(t)

	objs1, err := New("img", testNamespace, testOptions()).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())
	objs2, err := New("img", testNamespace, testOptions()).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	for _, name := range []string{ConfigMapName(testResourceType), BrokerConfigMapName(testResourceType)} {
		g.Expect(findConfigMap(objs1, name).Data).To(Equal(findConfigMap(objs2, name).Data))
	}
}

func TestRenderGooglePubSubBrokerConfig(t *testing.T) {
	g := NewWithT(t)

	opts := testOptions()
	opts.BrokerType = BrokerTypeGooglePubSub
	opts.BrokerProjectID = "my-gcp-project"

	objs, err := New("img", testNamespace, opts).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	bcm := findConfigMap(objs, BrokerConfigMapName(testResourceType))
	g.Expect(bcm).NotTo(BeNil())
	g.Expect(bcm.Data[brokerConfigFileKey]).To(ContainSubstring("type: googlepubsub"))
	g.Expect(bcm.Data[brokerConfigFileKey]).To(ContainSubstring("project_id: my-gcp-project"))

	// Google Pub/Sub uses Workload Identity: no Secret env var, no SecretRoles.
	dep, ok := byKind(objs)["Deployment"].(*appsv1.Deployment)
	g.Expect(ok).To(BeTrue())
	for _, e := range dep.Spec.Template.Spec.Containers[0].Env {
		g.Expect(e.Name).NotTo(Equal(brokerSecretEnv))
	}
	g.Expect(New("img", testNamespace, opts).Rollout().SecretRoles).To(BeEmpty())
}

func TestRenderGooglePubSubRequiresProjectID(t *testing.T) {
	g := NewWithT(t)

	opts := testOptions()
	opts.BrokerType = BrokerTypeGooglePubSub

	_, err := New("img", testNamespace, opts).Render(context.Background(), testCR())
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring(BrokerGooglePubSubProjectIDEnv))
}

func TestRenderUnknownBrokerTypeErrors(t *testing.T) {
	g := NewWithT(t)

	opts := testOptions()
	opts.BrokerType = "kafka"

	_, err := New("img", testNamespace, opts).Render(context.Background(), testCR())
	g.Expect(err).To(HaveOccurred())
}

func TestRenderEmptyBrokerTypeDefaultsToRabbitMQ(t *testing.T) {
	g := NewWithT(t)

	objs, err := New("img", testNamespace, testOptions()).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	bcm := findConfigMap(objs, BrokerConfigMapName(testResourceType))
	g.Expect(bcm).NotTo(BeNil())
	g.Expect(bcm.Data[brokerConfigFileKey]).To(ContainSubstring("type: rabbitmq"))
	g.Expect(New("img", testNamespace, testOptions()).Rollout().SecretRoles).To(ConsistOf(component.SecretRoleBroker))
}

func TestRolloutNamesTheRenderedConfigAndDeployment(t *testing.T) {
	g := NewWithT(t)

	targets := New("img", testNamespace, testOptions()).Rollout()
	objs, err := New("img", testNamespace, testOptions()).Render(context.Background(), testCR())
	g.Expect(err).NotTo(HaveOccurred())

	cm := findConfigMap(objs, targets.ConfigMapName)
	g.Expect(cm).NotTo(BeNil())
	g.Expect(cm.Data).To(HaveKey(targets.ConfigFileKey))

	dep, ok := byKind(objs)["Deployment"].(*appsv1.Deployment)
	g.Expect(ok).To(BeTrue())
	g.Expect(targets.DeploymentName).To(Equal(dep.Name))

	g.Expect(targets.SecretRoles).To(ConsistOf(component.SecretRoleBroker))
}

// deploymentWithStatus returns a Deployment with the given replica and rollout
// status fields, for feeding directly into Conditions.
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

func TestConditionsNoDeployment(t *testing.T) {
	g := NewWithT(t)

	conds, err := New("img", testNamespace, testOptions()).Conditions(context.Background(), testCR(), nil)
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
	conds, err := New("img", testNamespace, testOptions()).Conditions(context.Background(), testCR(), []client.Object{dep})
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Status).To(Equal(metav1.ConditionFalse))
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Reason).To(Equal(hyperfleetv1alpha1.ReasonDeploymentUnavailable))
}

func TestConditionsPartiallyReady(t *testing.T) {
	g := NewWithT(t)

	dep := deploymentWithStatus(2, 2, 1, 1, 1)
	conds, err := New("img", testNamespace, testOptions()).Conditions(context.Background(), testCR(), []client.Object{dep})
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Status).To(Equal(metav1.ConditionFalse))
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Reason).To(Equal(hyperfleetv1alpha1.ReasonDeploymentNotReady))
	g.Expect(byType[hyperfleetv1alpha1.ConditionProgressing].Status).To(Equal(metav1.ConditionTrue))
}

func TestConditionsFullyReadyAndStable(t *testing.T) {
	g := NewWithT(t)

	dep := deploymentWithStatus(1, 1, 1, 1, 1)
	conds, err := New("img", testNamespace, testOptions()).Conditions(context.Background(), testCR(), []client.Object{dep})
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
	conds, err := New("img", testNamespace, testOptions()).Conditions(context.Background(), testCR(), []client.Object{dep})
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionProgressing].Status).To(Equal(metav1.ConditionTrue))
	g.Expect(byType[hyperfleetv1alpha1.ConditionProgressing].Reason).To(Equal(hyperfleetv1alpha1.ReasonRolloutInProgress))
}

func TestConditionsFindsDeploymentInMixedKindSlice(t *testing.T) {
	g := NewWithT(t)

	name := ResourceName(testResourceType)
	dep := deploymentWithStatus(1, 1, 1, 1, 1)
	applied := []client.Object{
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName(testResourceType)}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: BrokerConfigMapName(testResourceType)}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: name}},
		dep,
	}

	conds, err := New("img", testNamespace, testOptions()).Conditions(context.Background(), testCR(), applied)
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Status).To(Equal(metav1.ConditionTrue))
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Reason).To(Equal(hyperfleetv1alpha1.ReasonDeploymentAvailable))
}

func TestConditionsNilSpecReplicasDefaultsToOne(t *testing.T) {
	g := NewWithT(t)

	dep := deploymentWithStatus(1, 1, 1, 1, 1)
	dep.Spec.Replicas = nil

	conds, err := New("img", testNamespace, testOptions()).Conditions(context.Background(), testCR(), []client.Object{dep})
	g.Expect(err).NotTo(HaveOccurred())

	byType := condMapByType(conds)
	g.Expect(byType[hyperfleetv1alpha1.ConditionAvailable].Status).To(Equal(metav1.ConditionTrue))
	g.Expect(byType[hyperfleetv1alpha1.ConditionProgressing].Status).To(Equal(metav1.ConditionFalse))
}
