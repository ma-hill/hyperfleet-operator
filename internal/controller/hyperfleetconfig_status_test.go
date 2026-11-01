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
	"errors"
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hyperfleetv1alpha1 "github.com/openshift-hyperfleet/hyperfleet-operator/api/v1alpha1"
)

func condition(condType string, status metav1.ConditionStatus, reason string) metav1.Condition {
	return metav1.Condition{Type: condType, Status: status, Reason: reason, Message: "test"}
}

// componentEntry wraps conditions in the per-component shape Reconcile
// collects: one entry per component, carrying that component's conditions.
func componentEntry(name string, conds ...metav1.Condition) hyperfleetv1alpha1.ComponentConditions {
	return hyperfleetv1alpha1.ComponentConditions{Name: name, Conditions: conds}
}

// componentConditionByType returns the named component's condition of the
// given type from status.components, or the zero Condition if absent.
func componentConditionByType(cr *hyperfleetv1alpha1.HyperFleetConfig, name, condType string) metav1.Condition {
	for _, entry := range cr.Status.Components {
		if entry.Name != name {
			continue
		}
		for _, c := range entry.Conditions {
			if c.Type == condType {
				return c
			}
		}
	}
	return metav1.Condition{}
}

func conditionByType(cr *hyperfleetv1alpha1.HyperFleetConfig, condType string) metav1.Condition {
	for _, c := range cr.Status.Conditions {
		if c.Type == condType {
			return c
		}
	}
	return metav1.Condition{}
}

func TestAggregateStatusNoBumpOnUnrelatedReconcile(t *testing.T) {
	g := NewWithT(t)

	cr := &hyperfleetv1alpha1.HyperFleetConfig{}
	cr.Generation = 1
	componentConditions := []hyperfleetv1alpha1.ComponentConditions{
		componentEntry("api",
			condition(hyperfleetv1alpha1.ConditionAvailable, metav1.ConditionTrue, hyperfleetv1alpha1.ReasonDeploymentAvailable),
			condition(hyperfleetv1alpha1.ConditionProgressing, metav1.ConditionFalse, hyperfleetv1alpha1.ReasonRolloutComplete)),
	}

	changed := aggregateStatus(cr, componentConditions, true, nil, nil)
	g.Expect(changed).To(BeTrue(), "first write must report a change")
	firstTransition := conditionByType(cr, hyperfleetv1alpha1.ConditionAvailable).LastTransitionTime

	// Second reconcile: Generation moved (an unrelated spec/annotation edit) but
	// component health did not — observedGeneration must still advance
	// (changed=true) while LastTransitionTime must stay byte-identical (AC#5).
	cr.Generation = 2
	changed = aggregateStatus(cr, componentConditions, true, nil, nil)
	g.Expect(changed).To(BeTrue(), "observedGeneration must still advance even though health is unchanged")
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionAvailable).LastTransitionTime).To(Equal(firstTransition))
	g.Expect(cr.Status.ObservedGeneration).To(Equal(int64(2)))

	// A third reconcile with nothing changed at all (same generation, same
	// health) must report no change whatsoever.
	changed = aggregateStatus(cr, componentConditions, true, nil, nil)
	g.Expect(changed).To(BeFalse(), "a fully unrelated no-op reconcile must not report a change")
}

func TestAggregateStatusAvailableIsAndOverComponents(t *testing.T) {
	g := NewWithT(t)

	cr := &hyperfleetv1alpha1.HyperFleetConfig{}
	cr.Generation = 1
	componentConditions := []hyperfleetv1alpha1.ComponentConditions{
		componentEntry("api",
			condition(hyperfleetv1alpha1.ConditionAvailable, metav1.ConditionTrue, hyperfleetv1alpha1.ReasonDeploymentAvailable)),
		componentEntry("sentinel-clusters",
			condition(hyperfleetv1alpha1.ConditionAvailable, metav1.ConditionTrue, hyperfleetv1alpha1.ReasonDeploymentAvailable)),
		componentEntry("sentinel-nodepools",
			condition(hyperfleetv1alpha1.ConditionAvailable, metav1.ConditionFalse, hyperfleetv1alpha1.ReasonDeploymentNotReady)),
	}

	aggregateStatus(cr, componentConditions, true, nil, nil)

	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionAvailable).Status).To(Equal(metav1.ConditionFalse))
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionAvailable).Reason).To(Equal(hyperfleetv1alpha1.ReasonDeploymentNotReady))
}

func TestAggregateStatusProgressingIsOrOverComponents(t *testing.T) {
	g := NewWithT(t)

	cr := &hyperfleetv1alpha1.HyperFleetConfig{}
	cr.Generation = 1
	componentConditions := []hyperfleetv1alpha1.ComponentConditions{
		componentEntry("api",
			condition(hyperfleetv1alpha1.ConditionProgressing, metav1.ConditionFalse, hyperfleetv1alpha1.ReasonRolloutComplete)),
		componentEntry("sentinel-clusters",
			condition(hyperfleetv1alpha1.ConditionProgressing, metav1.ConditionFalse, hyperfleetv1alpha1.ReasonRolloutComplete)),
		componentEntry("sentinel-nodepools",
			condition(hyperfleetv1alpha1.ConditionProgressing, metav1.ConditionTrue, hyperfleetv1alpha1.ReasonRolloutInProgress)),
	}

	aggregateStatus(cr, componentConditions, true, nil, nil)

	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionProgressing).Status).To(Equal(metav1.ConditionTrue))
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionProgressing).Reason).To(Equal(hyperfleetv1alpha1.ReasonRolloutInProgress))
}

func TestAggregateStatusDegradedPrecedence(t *testing.T) {
	g := NewWithT(t)

	cr := &hyperfleetv1alpha1.HyperFleetConfig{}
	cr.Generation = 1

	// missingSecrets and a reconcile error both set: missingSecrets takes
	// precedence in the surfaced reason (it is usually the root cause).
	aggregateStatus(cr, nil, false, []string{"database"}, errors.New("apply component \"api\": boom"))
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionDegraded).Status).To(Equal(metav1.ConditionTrue))
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionDegraded).Reason).To(Equal(hyperfleetv1alpha1.ReasonReferencedSecretMissing))
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionDegraded).Message).To(ContainSubstring("database"))

	// Only a reconcile error: ReconcileError, with a safe static message (never
	// the raw wrapped error text — see degradedCondition's doc comment).
	cr2 := &hyperfleetv1alpha1.HyperFleetConfig{}
	cr2.Generation = 1
	aggregateStatus(cr2, nil, false, nil, errors.New("apply component \"api\": boom"))
	g.Expect(conditionByType(cr2, hyperfleetv1alpha1.ConditionDegraded).Status).To(Equal(metav1.ConditionTrue))
	g.Expect(conditionByType(cr2, hyperfleetv1alpha1.ConditionDegraded).Reason).To(Equal(hyperfleetv1alpha1.ReasonReconcileError))
	g.Expect(conditionByType(cr2, hyperfleetv1alpha1.ConditionDegraded).Message).NotTo(ContainSubstring("boom"))

	// Neither: AsExpected/False.
	cr3 := &hyperfleetv1alpha1.HyperFleetConfig{}
	cr3.Generation = 1
	aggregateStatus(cr3, nil, false, nil, nil)
	g.Expect(conditionByType(cr3, hyperfleetv1alpha1.ConditionDegraded).Status).To(Equal(metav1.ConditionFalse))
	g.Expect(conditionByType(cr3, hyperfleetv1alpha1.ConditionDegraded).Reason).To(Equal(hyperfleetv1alpha1.ReasonAsExpected))
}

func TestAggregateStatusSetsObservedGeneration(t *testing.T) {
	g := NewWithT(t)

	cr := &hyperfleetv1alpha1.HyperFleetConfig{}
	cr.Generation = 3

	// componentsCollected=true with zero component conditions models a bundle
	// that genuinely has no components — the vacuous-true default is correct
	// here, unlike the componentsCollected=false case tested below.
	changed := aggregateStatus(cr, nil, true, nil, nil)
	g.Expect(changed).To(BeTrue())
	g.Expect(cr.Status.ObservedGeneration).To(Equal(int64(3)))
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionAvailable).Status).To(Equal(metav1.ConditionTrue))
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionAvailable).Reason).To(Equal(hyperfleetv1alpha1.ReasonDeploymentAvailable))
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionProgressing).Status).To(Equal(metav1.ConditionFalse))
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionProgressing).Reason).To(Equal(hyperfleetv1alpha1.ReasonRolloutComplete))
	for _, c := range cr.Status.Conditions {
		g.Expect(c.ObservedGeneration).To(Equal(int64(3)))
	}

	// A later reconcile at the same generation, same health: no change.
	changed = aggregateStatus(cr, nil, true, nil, nil)
	g.Expect(changed).To(BeFalse())
}

// TestAggregateStatusComponentsNotCollectedLeavesAvailableProgressingUnset
// covers the bug this story shipped and fixed: when Reconcile aborts before
// any component reports health (componentsCollected=false), Available and
// Progressing must NOT be fabricated as healthy — only Degraded is written.
func TestAggregateStatusComponentsNotCollectedLeavesAvailableProgressingUnset(t *testing.T) {
	g := NewWithT(t)

	cr := &hyperfleetv1alpha1.HyperFleetConfig{}
	cr.Generation = 1

	changed := aggregateStatus(cr, nil, false, nil, errors.New("resolve JWKS URL: boom"))
	g.Expect(changed).To(BeTrue(), "Degraded must still be written")

	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionDegraded).Status).To(Equal(metav1.ConditionTrue))
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionDegraded).Reason).To(Equal(hyperfleetv1alpha1.ReasonReconcileError))
	g.Expect(cr.Status.ObservedGeneration).To(Equal(int64(0)),
		"top-level observedGeneration must not advance when component health was never collected")
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionAvailable)).To(Equal(metav1.Condition{}),
		"Available must not be fabricated when components were never collected")
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionProgressing)).To(Equal(metav1.Condition{}),
		"Progressing must not be fabricated when components were never collected")
}

// TestAggregateStatusPublishesComponentConditions covers the per-component
// status surface (HYPERFLEET-1653): each component's conditions are published
// under its name in status.components, sorted for deterministic output, with
// observedGeneration stamped and LastTransitionTime stable across reconciles
// that do not change health — mirroring the top-level condition semantics.
func TestAggregateStatusPublishesComponentConditions(t *testing.T) {
	g := NewWithT(t)

	cr := &hyperfleetv1alpha1.HyperFleetConfig{}
	cr.Generation = 1
	// Deliberately unsorted input: the bundle's component order must not leak
	// into the published list.
	entries := []hyperfleetv1alpha1.ComponentConditions{
		componentEntry("sentinel-clusters",
			condition(hyperfleetv1alpha1.ConditionAvailable, metav1.ConditionFalse, hyperfleetv1alpha1.ReasonDeploymentNotReady)),
		componentEntry("api",
			condition(hyperfleetv1alpha1.ConditionAvailable, metav1.ConditionTrue, hyperfleetv1alpha1.ReasonDeploymentAvailable),
			condition(hyperfleetv1alpha1.ConditionProgressing, metav1.ConditionFalse, hyperfleetv1alpha1.ReasonRolloutComplete)),
	}

	changed := aggregateStatus(cr, entries, true, nil, nil)
	g.Expect(changed).To(BeTrue(), "first write must report a change")
	g.Expect(cr.Status.Components).To(HaveLen(2))
	g.Expect(cr.Status.Components[0].Name).To(Equal("api"), "entries must be sorted by name")
	g.Expect(cr.Status.Components[1].Name).To(Equal("sentinel-clusters"))
	for _, entry := range cr.Status.Components {
		for _, c := range entry.Conditions {
			g.Expect(c.ObservedGeneration).To(Equal(int64(1)))
			g.Expect(c.LastTransitionTime).NotTo(BeZero(),
				"published component conditions must carry a LastTransitionTime")
		}
	}
	apiAvailable := componentConditionByType(cr, "api", hyperfleetv1alpha1.ConditionAvailable)
	sentinelAvailable := componentConditionByType(cr, "sentinel-clusters", hyperfleetv1alpha1.ConditionAvailable)
	g.Expect(apiAvailable.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(sentinelAvailable.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(componentConditionByType(cr, "api", hyperfleetv1alpha1.ConditionProgressing).Status).
		To(Equal(metav1.ConditionFalse))

	// Same generation, same health: no change, and transition times are
	// preserved byte-identically.
	changed = aggregateStatus(cr, entries, true, nil, nil)
	g.Expect(changed).To(BeFalse(), "an unchanged reconcile must not report a change")
	g.Expect(componentConditionByType(cr, "api", hyperfleetv1alpha1.ConditionAvailable).LastTransitionTime).
		To(Equal(apiAvailable.LastTransitionTime))

	// The sentinel component recovers: its entry flips, which must surface as
	// a change and bump only that component's transition time.
	entries[0].Conditions[0].Status = metav1.ConditionTrue
	entries[0].Conditions[0].Reason = hyperfleetv1alpha1.ReasonDeploymentAvailable
	changed = aggregateStatus(cr, entries, true, nil, nil)
	g.Expect(changed).To(BeTrue())
	g.Expect(componentConditionByType(cr, "sentinel-clusters", hyperfleetv1alpha1.ConditionAvailable).Status).
		To(Equal(metav1.ConditionTrue))
	g.Expect(componentConditionByType(cr, "sentinel-clusters", hyperfleetv1alpha1.ConditionAvailable).LastTransitionTime).
		NotTo(Equal(sentinelAvailable.LastTransitionTime))
	// The rollup follows: with every component available, top-level Available
	// is now True.
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionAvailable).Status).To(Equal(metav1.ConditionTrue))
}

// TestAggregateStatusComponentsNotCollectedPreservesPriorAvailable covers the
// same fix from the other direction: a transient failure on a LATER reconcile
// (after a prior successful one already recorded real health) must not
// overwrite that last-known-good Available/Progressing with a guess.
func TestAggregateStatusComponentsNotCollectedPreservesPriorAvailable(t *testing.T) {
	g := NewWithT(t)

	cr := &hyperfleetv1alpha1.HyperFleetConfig{}
	cr.Generation = 1

	// A prior, fully successful reconcile recorded real health.
	componentConditions := []hyperfleetv1alpha1.ComponentConditions{
		componentEntry("api",
			condition(hyperfleetv1alpha1.ConditionAvailable, metav1.ConditionTrue, hyperfleetv1alpha1.ReasonDeploymentAvailable),
			condition(hyperfleetv1alpha1.ConditionProgressing, metav1.ConditionFalse, hyperfleetv1alpha1.ReasonRolloutComplete)),
	}
	aggregateStatus(cr, componentConditions, true, nil, nil)
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionAvailable).Status).To(Equal(metav1.ConditionTrue))

	// A later reconcile fails before reaching any component (e.g. a transient
	// OIDC discovery blip): Available must remain exactly as last recorded.
	cr.Generation = 2
	aggregateStatus(cr, nil, false, nil, errors.New("resolve JWKS URL: boom"))

	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionAvailable).Status).To(Equal(metav1.ConditionTrue),
		"a failed reconcile that never reached the components must not change the last-known Available value")
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionDegraded).Status).To(Equal(metav1.ConditionTrue))
	g.Expect(cr.Status.ObservedGeneration).To(Equal(int64(1)),
		"top-level observedGeneration must stay pinned to the last fully processed generation")
	g.Expect(cr.Status.Components).To(HaveLen(1),
		"per-component status must be preserved when components were never collected")
	g.Expect(componentConditionByType(cr, "api", hyperfleetv1alpha1.ConditionAvailable).Status).
		To(Equal(metav1.ConditionTrue),
			"a failed reconcile that never reached the components must not change last-known component health")
}

// TestAggregateStatusEmptyReportIsPublishedAsBareEntry documents how the
// status treats a Conditions() contract violation (a component returning no
// conditions without an error — prevented at implementation time by the
// bundle-wide test in internal/bundle, logged at runtime by the controller):
// the component's entry is published with no conditions and it is invisible to
// the top-level rollup. This test pins that behavior as intentional rather
// than accidental; changing it (e.g. to a fallback to last-known health)
// requires updating docs/component-pattern.md and the reference guide.
func TestAggregateStatusEmptyReportIsPublishedAsBareEntry(t *testing.T) {
	g := NewWithT(t)

	cr := &hyperfleetv1alpha1.HyperFleetConfig{}
	cr.Generation = 1
	empty := []hyperfleetv1alpha1.ComponentConditions{
		componentEntry("api",
			condition(hyperfleetv1alpha1.ConditionAvailable, metav1.ConditionTrue, hyperfleetv1alpha1.ReasonDeploymentAvailable)),
		componentEntry("sentinel-clusters"),
	}
	changed := aggregateStatus(cr, empty, true, nil, nil)
	g.Expect(changed).To(BeTrue())
	g.Expect(cr.Status.ObservedGeneration).To(Equal(int64(1)))
	g.Expect(cr.Status.Components).To(HaveLen(2))
	g.Expect(cr.Status.Components[1].Conditions).To(BeEmpty(),
		"an empty report publishes a bare entry — a visible trace that the component reported nothing")
	// The silent component is invisible to the rollup: top-level Available is
	// computed from the reporting components only (vacuously true here).
	g.Expect(conditionByType(cr, hyperfleetv1alpha1.ConditionAvailable).Status).To(Equal(metav1.ConditionTrue))
}

// TestAggregateStatusSameCountNameSwapReportsChange covers the change
// detection added alongside the empty-report bare-entry handling: when a
// component name is replaced by a different name and both entries carry no
// conditions, the per-entry conditionsEqual comparison sees no difference and
// the list length is unchanged — only the name-set check notices the swap, so
// the status update is not skipped.
func TestAggregateStatusSameCountNameSwapReportsChange(t *testing.T) {
	g := NewWithT(t)

	cr := &hyperfleetv1alpha1.HyperFleetConfig{}
	cr.Generation = 1
	swapped := []hyperfleetv1alpha1.ComponentConditions{
		componentEntry("api",
			condition(hyperfleetv1alpha1.ConditionAvailable, metav1.ConditionTrue, hyperfleetv1alpha1.ReasonDeploymentAvailable)),
		componentEntry("sentinel-clusters"),
	}
	aggregateStatus(cr, swapped, true, nil, nil)
	g.Expect(cr.Status.Components).To(HaveLen(2))

	// Same count, different name, both entries without conditions.
	swapped[1].Name = "sentinel-nodepools"
	changed := aggregateStatus(cr, swapped, true, nil, nil)
	g.Expect(changed).To(BeTrue(),
		"a same-count bare-entry name swap must report a change or the persisted status keeps the stale name")
	g.Expect(cr.Status.Components[1].Name).To(Equal("sentinel-nodepools"))
}
