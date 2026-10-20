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

// Package sentinel renders one Sentinel instance, the second bundle component
// after the API (HYPERFLEET-1673). Builders here are pure functions; the
// controller applies what Render produces. Operand shapes are translated from
// the hyperfleet-sentinel Helm chart, with deliberate departures: no Service
// (no in-cluster caller), one operand set per watched resource type (the
// binary watches exactly one; internal/bundle constructs one Component per
// entity), no Role/RoleBinding (Sentinel never calls the Kubernetes API; the
// chart renders none), and a fixed-name broker credential Secret (see
// sentinel.go).
package sentinel

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const (
	managedByOperator = "hyperfleet-operator"
	partOfHyperfleet  = "hyperfleet"

	// The http port serves only /healthz and /readyz; Sentinel is an outbound
	// poller, not a request handler.
	portNameHTTP    = "http"
	portNameMetrics = "metrics"
	portHTTP        = 8080
	portMetrics     = 9090

	configDir             = "/etc/hyperfleet"
	configMountPath       = configDir + "/" + ConfigFileKey
	brokerConfigMountPath = configDir + "/" + BrokerConfigFileKey
	configVolume          = "config"
	brokerConfigVolume    = "broker-config"
	tmpVolume             = "tmp"

	brokerSecretEnv = "BROKER_RABBITMQ_URL"
	brokerConfigEnv = "BROKER_CONFIG_FILE"
)

// Label keys, declared once to keep the common and selector sets in sync.
const (
	labelName      = "app.kubernetes.io/name"
	labelInstance  = "app.kubernetes.io/instance"
	labelComponent = "app.kubernetes.io/component"
	labelPartOf    = "app.kubernetes.io/part-of"
	labelManagedBy = "app.kubernetes.io/managed-by"
)

// ResourceName is the metadata.name of a resource type's non-ConfigMap operands.
func ResourceName(resourceType string) string {
	return "hyperfleet-sentinel-" + resourceType
}

// ConfigMapName is the metadata.name of a resource type's config ConfigMap.
func ConfigMapName(resourceType string) string {
	return ResourceName(resourceType) + "-config"
}

// BrokerConfigMapName is the metadata.name of a resource type's broker-config
// ConfigMap, matching the chart's two-ConfigMap split.
func BrokerConfigMapName(resourceType string) string {
	return ResourceName(resourceType) + "-broker-config"
}

// componentName returns this instance's identity, matching Component.Name().
func componentName(resourceType string) string {
	return ComponentName + "-" + resourceType
}

func labels(crName, resourceType string) map[string]string {
	name := componentName(resourceType)
	return map[string]string{
		labelName:      name,
		labelInstance:  crName,
		labelComponent: name,
		labelPartOf:    partOfHyperfleet,
		labelManagedBy: managedByOperator,
	}
}

// selectorLabels is the immutable subset used by the Deployment selector, pod
// template and NetworkPolicy; a Deployment selector cannot change after
// creation.
func selectorLabels(crName, resourceType string) map[string]string {
	name := componentName(resourceType)
	return map[string]string{
		labelName:      name,
		labelInstance:  crName,
		labelComponent: name,
	}
}

func containerResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}
}

func containerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
}

// deployment builds a resource type's Sentinel Deployment. For RabbitMQ the
// broker URL comes from a Secret-sourced env var; Google Pub/Sub authenticates
// via Workload Identity and needs no credential. The config-hash pod-template
// annotation is added by the controller, not here.
func deployment(crName, image, namespace, resourceType string, brokerType BrokerType) *appsv1.Deployment {
	name := ResourceName(resourceType)

	env := []corev1.EnvVar{
		{Name: brokerConfigEnv, Value: brokerConfigMountPath},
	}
	if brokerType == BrokerTypeRabbitMQ {
		env = append(env, corev1.EnvVar{
			Name: brokerSecretEnv,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: BrokerSecretName},
					Key:                  BrokerSecretKey,
				},
			},
		})
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: configVolume, MountPath: configMountPath, SubPath: ConfigFileKey, ReadOnly: true},
		{Name: brokerConfigVolume, MountPath: brokerConfigMountPath, SubPath: BrokerConfigFileKey, ReadOnly: true},
		{Name: tmpVolume, MountPath: "/tmp"},
	}
	volumes := []corev1.Volume{
		{
			Name: configVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: ConfigMapName(resourceType)},
				},
			},
		},
		{
			Name: brokerConfigVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: BrokerConfigMapName(resourceType)},
				},
			},
		},
		{
			Name:         tmpVolume,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels(crName, resourceType),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels(crName, resourceType)},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge:       ptr.To(intstr.FromInt32(1)),
					MaxUnavailable: ptr.To(intstr.FromInt32(0)),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels(crName, resourceType)},
				Spec: corev1.PodSpec{
					// Sentinel never calls the Kubernetes API, so no
					// service-account token is needed.
					AutomountServiceAccountToken:  ptr.To(false),
					ServiceAccountName:            name,
					TerminationGracePeriodSeconds: ptr.To[int64](30),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
					},
					Containers: []corev1.Container{{
						Name:            "sentinel",
						Image:           image,
						ImagePullPolicy: corev1.PullAlways,
						Args:            []string{"serve", "--config=" + configMountPath},
						Ports: []corev1.ContainerPort{
							{Name: portNameHTTP, ContainerPort: portHTTP, Protocol: corev1.ProtocolTCP},
							{Name: portNameMetrics, ContainerPort: portMetrics, Protocol: corev1.ProtocolTCP},
						},
						Env: env,
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromString(portNameHTTP)},
							},
							InitialDelaySeconds: 15,
							PeriodSeconds:       20,
							TimeoutSeconds:      5,
							FailureThreshold:    3,
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromString(portNameHTTP)},
							},
							InitialDelaySeconds: 5,
							PeriodSeconds:       10,
							TimeoutSeconds:      3,
							FailureThreshold:    3,
						},
						Resources:       containerResources(),
						SecurityContext: containerSecurityContext(),
						VolumeMounts:    volumeMounts,
					}},
					Volumes: volumes,
				},
			},
		},
	}
}

func serviceAccount(crName, namespace, resourceType string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ResourceName(resourceType),
			Namespace: namespace,
			Labels:    labels(crName, resourceType),
		},
	}
}

// configMap holds the rendered config.yaml; it is the ConfigMap Rollout()
// hashes.
func configMap(crName, namespace, resourceType, configYAML string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigMapName(resourceType),
			Namespace: namespace,
			Labels:    labels(crName, resourceType),
		},
		Data: map[string]string{
			ConfigFileKey: configYAML,
		},
	}
}

// brokerConfigMap holds the rendered broker.yaml.
func brokerConfigMap(crName, namespace, resourceType, brokerYAML string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      BrokerConfigMapName(resourceType),
			Namespace: namespace,
			Labels:    labels(crName, resourceType),
		},
		Data: map[string]string{
			BrokerConfigFileKey: brokerYAML,
		},
	}
}

// networkPolicy denies all ingress and leaves egress unrestricted: no Service
// is rendered, and outbound destinations (API, broker, DNS) may be
// cross-namespace or external.
func networkPolicy(crName, namespace, resourceType string) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ResourceName(resourceType),
			Namespace: namespace,
			Labels:    labels(crName, resourceType),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: selectorLabels(crName, resourceType)},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress:     []networkingv1.NetworkPolicyIngressRule{},
		},
	}
}
