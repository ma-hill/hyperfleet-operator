#!/usr/bin/env bash
set -euo pipefail

CSV_FILE="${CSV_FILE:-/manifests/hyperfleet-operator.clusterserviceversion.yaml}"

# Update image references in the CSV file using yq
yq eval '
  # Update operator deployment image
  (.spec.install.spec.deployments[].spec.template.spec.containers[] | select(.name == "manager") | .image) = strenv(HYPERFLEET_OPERATOR_IMAGE_PULLSPEC) |

  # Update RELATED_IMAGE_HYPERFLEET_API env var
  (.spec.install.spec.deployments[].spec.template.spec.containers[] | select(.name == "manager") | .env[] | select(.name == "RELATED_IMAGE_HYPERFLEET_API") | .value) = strenv(HYPERFLEET_API_IMAGE_PULLSPEC) |

  # Update containerImage annotation
  .metadata.annotations.containerImage = strenv(HYPERFLEET_OPERATOR_IMAGE_PULLSPEC) |

  # Update relatedImages
  .spec.relatedImages = [
    {"name": "hyperfleet-operator", "image": strenv(HYPERFLEET_OPERATOR_IMAGE_PULLSPEC)},
    {"name": "hyperfleet-api", "image": strenv(HYPERFLEET_API_IMAGE_PULLSPEC)}
  ]
' -i "${CSV_FILE}"

cat "${CSV_FILE}"
