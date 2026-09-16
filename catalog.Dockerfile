# Building stage
FROM quay.io/konflux-ci/operator-sdk-builder:latest@sha256:bd34ca58b2d08e8ee3b9cdf46b32f69173084ca09c1d3aba47285e2c35b4d1fc AS builder
WORKDIR /workspace
ARG TEMPLATEFILE
COPY catalog/base-template.yaml ./
COPY catalog/${TEMPLATEFILE} ./
RUN cat base-template.yaml ${TEMPLATEFILE} > ./template.yaml
RUN /bin/opm alpha render-template basic \
        --migrate-level=bundle-object-to-csv-metadata \
        -o yaml ./template.yaml > catalog.yaml

# OLM's catalog pod probes exec grpc_health_probe (still true through OCP 4.21)
ARG GRPC_HEALTH_PROBE_VERSION=v0.4.56
RUN CGO_ENABLED=0 GOFLAGS=-mod=mod GOBIN=/workspace/bin \
        go install github.com/grpc-ecosystem/grpc-health-probe@${GRPC_HEALTH_PROBE_VERSION}

# Final serving stage
FROM registry.access.redhat.com/ubi9/ubi-micro:latest AS serve
COPY --from=builder /bin/opm /bin/opm
COPY --from=builder /workspace/bin/grpc-health-probe /bin/grpc_health_probe
COPY --from=builder /workspace/catalog.yaml /configs/hyperfleet-operator/catalog.yaml
RUN ["/bin/opm", "serve", "/configs/hyperfleet-operator", "--cache-dir=/tmp/cache", "--cache-only"]
ENTRYPOINT ["/bin/opm"]
CMD ["serve", "/configs/hyperfleet-operator", "--cache-dir=/tmp/cache"]
ARG APP_VERSION="0.0.0-dev"
LABEL version="${APP_VERSION}"
LABEL operators.operatorframework.io.index.configs.v1=/configs
