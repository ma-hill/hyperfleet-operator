#!/usr/bin/env bash
set -euo pipefail

# Konflux nudges update these variables with the latest digest-pinned pullspecs.

export HYPERFLEET_OPERATOR_IMAGE_PULLSPEC="${HYPERFLEET_OPERATOR_IMAGE_PULLSPEC:-quay.io/openshift-hyperfleet/hyperfleet-operator:v0.0.1}"

CSV_FILE="${CSV_FILE:-/manifests/hyperfleet-operator.clusterserviceversion.yaml}"

# Update operator deployment image
yq eval ".spec.install.spec.deployments[].spec.template.spec.containers[] |= (
  select(.name == \"manager\") |
  .image = strenv(HYPERFLEET_OPERATOR_IMAGE_PULLSPEC)
)" -i "${CSV_FILE}"


# Update containerImage annotation
yq eval ".metadata.annotations.containerImage = strenv(HYPERFLEET_OPERATOR_IMAGE_PULLSPEC)" -i "${CSV_FILE}"

cat "${CSV_FILE}"
