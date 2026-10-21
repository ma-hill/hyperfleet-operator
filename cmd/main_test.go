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

package main

import (
	"reflect"
	"strings"
	"testing"

	sentinel "github.com/openshift-hyperfleet/hyperfleet-operator/internal/component/sentinel"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	envBrokerType      = "BROKER_TYPE"
	envPubSubProjectID = "BROKER_GOOGLEPUBSUB_PROJECT_ID"
	errInvalidProject  = "is not a valid GCP project ID"
	googlePubSubType   = "googlepubsub"
)

func TestNamespacedCacheByObject(t *testing.T) {
	t.Parallel()

	const operatorNamespace = "test-operator"
	got := namespacedCacheByObject(operatorNamespace)
	wantTypes := []reflect.Type{
		reflect.TypeFor[*corev1.Secret](),
		reflect.TypeFor[*appsv1.Deployment](),
		reflect.TypeFor[*corev1.Service](),
		reflect.TypeFor[*corev1.ServiceAccount](),
		reflect.TypeFor[*corev1.ConfigMap](),
		reflect.TypeFor[*rbacv1.Role](),
		reflect.TypeFor[*rbacv1.RoleBinding](),
		reflect.TypeFor[*networkingv1.NetworkPolicy](),
	}

	if len(got) != len(wantTypes) {
		t.Fatalf("namespacedCacheByObject() returned %d types, want %d", len(got), len(wantTypes))
	}
	for _, wantType := range wantTypes {
		var found bool
		for object, config := range got {
			if reflect.TypeOf(object) != wantType {
				continue
			}
			found = true
			if len(config.Namespaces) != 1 {
				t.Fatalf("cache config for %v has namespaces %v, want only %q", wantType, config.Namespaces, operatorNamespace)
			}
			if _, ok := config.Namespaces[operatorNamespace]; !ok {
				t.Fatalf("cache config for %v does not include %q: %v", wantType, operatorNamespace, config.Namespaces)
			}
			break
		}
		if !found {
			t.Errorf("namespacedCacheByObject() is missing %v", wantType)
		}
	}

}

func TestEnvToBrokerInfo_valid(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantType    sentinel.BrokerType
		wantProject string
	}{
		{
			name:     "no BROKER_TYPE defaults to rabbitmq",
			env:      map[string]string{},
			wantType: sentinel.BrokerTypeRabbitMQ,
		},
		{
			name:     "explicit rabbitmq",
			env:      map[string]string{envBrokerType: "rabbitmq"},
			wantType: sentinel.BrokerTypeRabbitMQ,
		},
		{
			name: "googlepubsub with valid project ID",
			env: map[string]string{
				envBrokerType:      googlePubSubType,
				envPubSubProjectID: "valid-gcp-project",
			},
			wantType:    sentinel.BrokerTypeGooglePubSub,
			wantProject: "valid-gcp-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got, err := envToBrokerInfo()
			if err != nil {
				t.Fatalf("envToBrokerInfo() unexpected error: %v", err)
			}
			if got.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tt.wantType)
			}
			if got.ProjectID != tt.wantProject {
				t.Errorf("ProjectID = %q, want %q", got.ProjectID, tt.wantProject)
			}
		})
	}
}

func TestEnvToBrokerInfo_invalid(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		errContains string
	}{
		{
			name:        "invalid BROKER_TYPE",
			env:         map[string]string{envBrokerType: "not-broker-type"},
			errContains: "be set to either 'rabbitmq' or 'googlepubsub'",
		},
		{
			name:        "googlepubsub missing project ID",
			env:         map[string]string{envBrokerType: googlePubSubType},
			errContains: "BROKER_GOOGLEPUBSUB_PROJECT_ID must be set",
		},
		{
			name:        "googlepubsub empty project ID",
			env:         map[string]string{envBrokerType: googlePubSubType, envPubSubProjectID: ""},
			errContains: "BROKER_GOOGLEPUBSUB_PROJECT_ID must be set",
		},
		{
			name: "googlepubsub project ID with uppercase",
			env: map[string]string{
				envBrokerType:      googlePubSubType,
				envPubSubProjectID: "GOOGLE-PROJECT_INVALID",
			},
			errContains: errInvalidProject,
		},
		{
			name: "googlepubsub project ID too short",
			env: map[string]string{
				envBrokerType:      googlePubSubType,
				envPubSubProjectID: "sm",
			},
			errContains: errInvalidProject,
		},
		{
			name: "googlepubsub project ID ending with hyphen",
			env: map[string]string{
				envBrokerType:      googlePubSubType,
				envPubSubProjectID: "my-project-",
			},
			errContains: errInvalidProject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			_, err := envToBrokerInfo()
			if err == nil {
				t.Fatal("envToBrokerInfo() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.errContains)
			}
		})
	}
}
