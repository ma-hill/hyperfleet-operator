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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
	"github.com/openshift-hyperfleet/hyperfleet-operator/internal/component"
)

// RelatedImageEnv is the release image override supplied by the bundle CSV.
const RelatedImageEnv = "RELATED_IMAGE_HYPERFLEET_SENTINEL"

// BrokerSecretName is the fixed name of the Secret holding the broker
// connection URL. There is no CR field for it — the broker is operator-internal
// (HYPERFLEET-1673); see docs/hyperfleetconfig-reference.md for the
// prerequisite.
const BrokerSecretName = "hyperfleet-broker-credentials"

// BrokerSecretKey is the data key holding the connection URL, matching the env
// var name the hyperfleet-sentinel binary expects.
const BrokerSecretKey = "BROKER_RABBITMQ_URL"

// Component renders one Sentinel instance, watching a single resource type. A
// bundle resolves to one Component per registered entity (one process watches
// one resource type). It carries only rendering inputs and never touches the
// cluster; the controller applies what Render produces.
type Component struct {
	// Image is the Sentinel container image; Render errors when empty (no
	// compiled-in fallback, unlike the API component).
	Image string
	// Namespace is the operator's own namespace, where all operands live.
	Namespace string
	// ResourceType is the plural resource type this instance watches.
	ResourceType string
	// APIBaseURL is the in-cluster base URL of the API this instance polls.
	APIBaseURL string
	// BrokerType selects the broker; empty means DefaultBrokerType.
	BrokerType string
	// BrokerProjectID is the GCP project id, required when BrokerType is
	// BrokerTypeGooglePubSub.
	BrokerProjectID string
}

// Options carries the non-image, non-namespace inputs to New.
type Options struct {
	ResourceType    string
	APIBaseURL      string
	BrokerType      string
	BrokerProjectID string
}

// New constructs one Sentinel component instance.
func New(image, namespace string, opts Options) *Component {
	return &Component{
		Image:           image,
		Namespace:       namespace,
		ResourceType:    opts.ResourceType,
		APIBaseURL:      opts.APIBaseURL,
		BrokerType:      opts.BrokerType,
		BrokerProjectID: opts.BrokerProjectID,
	}
}

// brokerType returns c.BrokerType, defaulting to DefaultBrokerType when unset.
func (c *Component) brokerType() string {
	if c.BrokerType == "" {
		return DefaultBrokerType
	}
	return c.BrokerType
}

// Name identifies this instance in logs, metrics and labels; suffixed with
// ResourceType because a bundle resolves to several instances.
func (c *Component) Name() string {
	return fmt.Sprintf("%s-%s", ComponentName, c.ResourceType)
}

// Rollout names this instance's rollout-hash targets. Only RabbitMQ declares
// the broker Secret role; Google Pub/Sub authenticates via Workload Identity.
func (c *Component) Rollout() component.RolloutTargets {
	targets := component.RolloutTargets{
		ConfigMapName:  ConfigMapName(c.ResourceType),
		ConfigFileKey:  ConfigFileKey,
		DeploymentName: ResourceName(c.ResourceType),
	}
	if c.brokerType() == BrokerTypeRabbitMQ {
		targets.SecretRoles = []string{component.SecretRoleBroker}
	}
	return targets
}

// Render returns this instance's operand set in dependency order. Pure: no
// cluster reads or writes.
func (c *Component) Render(_ context.Context, cr *hyperfleetv1alpha1.HyperFleetConfig) ([]client.Object, error) {
	// No compiled-in default: fail loudly rather than render a dev tag.
	if c.Image == "" {
		return nil, fmt.Errorf("sentinel image is required")
	}

	configYAML, err := renderConfig(configInput{
		ResourceType: c.ResourceType,
		APIBaseURL:   c.APIBaseURL,
		Namespace:    c.Namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("render sentinel config for %q: %w", c.ResourceType, err)
	}
	brokerYAML, err := renderBrokerConfig(brokerConfigInput{
		BrokerType:      c.brokerType(),
		BrokerProjectID: c.BrokerProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("render sentinel broker config for %q: %w", c.ResourceType, err)
	}

	return []client.Object{
		serviceAccount(cr, c.Namespace, c.ResourceType),
		configMap(cr, c.Namespace, c.ResourceType, configYAML),
		brokerConfigMap(cr, c.Namespace, c.ResourceType, brokerYAML),
		networkPolicy(cr, c.Namespace, c.ResourceType),
		deployment(cr, c.Image, c.Namespace, c.ResourceType, c.brokerType()),
	}, nil
}

// Conditions derives health from the applied Deployment (refreshed in place by
// apply.Objects). Identical to api.Conditions — both are Deployment-backed
// workloads with no further readiness signal of their own.
func (c *Component) Conditions(_ context.Context, _ *hyperfleetv1alpha1.HyperFleetConfig, applied []client.Object) ([]metav1.Condition, error) {
	dep := findDeployment(applied, ResourceName(c.ResourceType))
	return []metav1.Condition{
		availableCondition(dep, c.ResourceType),
		progressingCondition(dep, c.ResourceType),
	}, nil
}

// findDeployment returns the named Deployment from applied, or nil if absent.
func findDeployment(applied []client.Object, name string) *appsv1.Deployment {
	for _, o := range applied {
		if dep, ok := o.(*appsv1.Deployment); ok && dep.Name == name {
			return dep
		}
	}
	return nil
}

// desiredReplicas returns dep.Spec.Replicas, defaulting to 1.
func desiredReplicas(dep *appsv1.Deployment) int32 {
	if dep.Spec.Replicas != nil {
		return *dep.Spec.Replicas
	}
	return 1
}

// availableCondition derives Available from the Deployment's replica counts;
// see api.availableCondition for the rationale.
func availableCondition(dep *appsv1.Deployment, resourceType string) metav1.Condition {
	if dep == nil {
		return metav1.Condition{
			Type:    hyperfleetv1alpha1.ConditionAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  hyperfleetv1alpha1.ReasonDeploymentUnavailable,
			Message: fmt.Sprintf("the Sentinel (%s) Deployment does not exist", resourceType),
		}
	}

	desired := desiredReplicas(dep)

	switch {
	case desired == 0:
		return metav1.Condition{
			Type:    hyperfleetv1alpha1.ConditionAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  hyperfleetv1alpha1.ReasonDeploymentAvailable,
			Message: fmt.Sprintf("the Sentinel (%s) Deployment is scaled to zero", resourceType),
		}
	case dep.Status.AvailableReplicas == 0:
		return metav1.Condition{
			Type:    hyperfleetv1alpha1.ConditionAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  hyperfleetv1alpha1.ReasonDeploymentUnavailable,
			Message: fmt.Sprintf("the Sentinel (%s) Deployment has no available replicas", resourceType),
		}
	case dep.Status.AvailableReplicas < desired:
		return metav1.Condition{
			Type:    hyperfleetv1alpha1.ConditionAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  hyperfleetv1alpha1.ReasonDeploymentNotReady,
			Message: fmt.Sprintf("the Sentinel (%s) Deployment has %d/%d replicas available", resourceType, dep.Status.AvailableReplicas, desired),
		}
	default:
		return metav1.Condition{
			Type:    hyperfleetv1alpha1.ConditionAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  hyperfleetv1alpha1.ReasonDeploymentAvailable,
			Message: fmt.Sprintf("the Sentinel (%s) Deployment is available", resourceType),
		}
	}
}

// progressingCondition derives Progressing from the same signals Kubernetes
// uses for rollout completion; see api.progressingCondition.
func progressingCondition(dep *appsv1.Deployment, resourceType string) metav1.Condition {
	if dep == nil {
		return metav1.Condition{
			Type:    hyperfleetv1alpha1.ConditionProgressing,
			Status:  metav1.ConditionTrue,
			Reason:  hyperfleetv1alpha1.ReasonRolloutInProgress,
			Message: fmt.Sprintf("the Sentinel (%s) Deployment does not exist yet", resourceType),
		}
	}

	desired := desiredReplicas(dep)

	if dep.Status.ObservedGeneration < dep.Generation ||
		dep.Status.UpdatedReplicas < desired ||
		dep.Status.Replicas != dep.Status.UpdatedReplicas ||
		dep.Status.AvailableReplicas < dep.Status.UpdatedReplicas {
		return metav1.Condition{
			Type:    hyperfleetv1alpha1.ConditionProgressing,
			Status:  metav1.ConditionTrue,
			Reason:  hyperfleetv1alpha1.ReasonRolloutInProgress,
			Message: fmt.Sprintf("the Sentinel (%s) Deployment rollout has not completed", resourceType),
		}
	}
	return metav1.Condition{
		Type:    hyperfleetv1alpha1.ConditionProgressing,
		Status:  metav1.ConditionFalse,
		Reason:  hyperfleetv1alpha1.ReasonRolloutComplete,
		Message: fmt.Sprintf("the Sentinel (%s) Deployment rollout is complete", resourceType),
	}
}
