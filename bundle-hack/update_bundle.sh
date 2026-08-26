#!/usr/bin/env bash
set -euo pipefail

# Konflux nudges update these variables with the latest digest-pinned pullspecs.

HYPERFLEET_OPERATOR_IMAGE_PULLSPEC="${HYPERFLEET_OPERATOR_IMAGE_PULLSPEC:-quay.io/openshift-hyperfleet/hyperfleet-operator:v0.0.1}"

CSV_FILE="${CSV_FILE:-/manifests/hyperfleet-operator.clusterserviceversion.yaml}"

# Update operator deployment image
yq eval ".spec.install.spec.deployments[].spec.template.spec.containers[] |= (
  select(.name == \"manager\") |
  .image = \"${HYPERFLEET_OPERATOR_IMAGE_PULLSPEC}\"
)" -i "${CSV_FILE}"


# Update containerImage annotation
yq eval ".metadata.annotations.containerImage = \"${HYPERFLEET_OPERATOR_IMAGE_PULLSPEC}\"" -i "${CSV_FILE}"

# Update relatedImages

cat "${CSV_FILE}"
