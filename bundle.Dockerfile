FROM registry.redhat.io/openshift4/ose-cli-rhel9:v4.18 AS kustomize

COPY config/ /workdir/config/

# Specify the kustomize variant, either bases/kustomization.yaml or prod/kustomization.yaml
# prod/kustomization.yaml gets image update references from konflux.
ARG KUSTOMIZE_VARIANT=config/manifests/dev
# ARG KUSTOMIZE_VARIANT=config/manifests/prod for konflux builds
RUN oc kustomize /workdir/${KUSTOMIZE_VARIANT} > /workdir/manifests.yaml

FROM registry.redhat.io/openshift4/ose-operator-sdk-rhel9:v4.18 AS operator
WORKDIR /workdir
COPY --from=kustomize /workdir/manifests.yaml /workdir/manifests.yaml
ARG CHANNELS=stable
ARG VERSION=0.0.1
RUN mkdir -p /workdir/bundle
RUN cat manifests.yaml | operator-sdk generate bundle -q --version ${VERSION} \
      --channels=${CHANNELS} --default-channel=stable \
      --package=hyperfleet-operator && \
    operator-sdk bundle validate ./bundle

FROM alpine

# Core bundle labels.
LABEL operators.operatorframework.io.bundle.mediatype.v1=registry+v1
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
LABEL operators.operatorframework.io.bundle.package.v1=hyperfleet-operator
LABEL operators.operatorframework.io.bundle.channels.v1=stable
LABEL operators.operatorframework.io.bundle.channel.default.v1=stable
LABEL operators.operatorframework.io.metrics.builder=operator-sdk-v1.42.3
LABEL operators.operatorframework.io.metrics.mediatype.v1=metrics+v1
LABEL operators.operatorframework.io.metrics.project_layout=go.kubebuilder.io/v4

# Labels for testing.
LABEL operators.operatorframework.io.test.mediatype.v1=scorecard+v1
LABEL operators.operatorframework.io.test.config.v1=tests/scorecard/

# Copy patched manifests from builder, metadata and tests from source.
COPY --from=operator /workdir/bundle/manifests /manifests/
COPY --from=operator /workdir/bundle/metadata /metadata/
COPY --from=operator /workdir/bundle/tests/scorecard /tests/scorecard/

ARG APP_VERSION="0.0.0-dev"
LABEL name="hyperfleet-operator-bundle" \
      vendor="Red Hat, Inc." \
      version="${APP_VERSION}" \
      summary="OLM bundle for the HyperFleet Operator" \
      description="OLM bundle for the HyperFleet Operator, which installs and manages HyperFleet." \
      com.redhat.component="hyperfleet-operator-bundle-container" \
      io.k8s.description="OLM bundle for the HyperFleet Operator, which installs and manages HyperFleet." \
      distribution-scope="public" \
      release="1" \
      url="https://github.com/openshift-hyperfleet/hyperfleet-operator" \
      maintainer="Red Hat HyperFleet Team"