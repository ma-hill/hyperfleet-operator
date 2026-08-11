ARG BASE_IMAGE=registry.access.redhat.com/ubi9-micro:latest

FROM registry.access.redhat.com/ubi9/go-toolset:9.8-1785443561 AS builder

ARG APP_VERSION="0.0.0-dev"

# Install make as root (UBI9 go-toolset doesn't include it), then switch back to non-root.
USER root
RUN dnf install -y make && dnf clean all
WORKDIR /build
RUN chown 1001:0 /build
USER 1001

ENV GOBIN=/build/.gobin
RUN mkdir -p $GOBIN
ENV PATH="${GOBIN}:${PATH}"

COPY --chown=1001:0 go.mod go.sum ./
RUN --mount=type=cache,target=/opt/app-root/src/go/pkg/mod,uid=1001 \
    go mod download

COPY --chown=1001:0 . .

# CGO_ENABLED=0 produces a static binary. The default ubi9-micro runtime
# supports both static and dynamically linked binaries.
# For FIPS-compliant builds, use CGO_ENABLED=1 + GOEXPERIMENT=boringcrypto.
RUN --mount=type=cache,target=/opt/app-root/src/go/pkg/mod,uid=1001 \
    --mount=type=cache,target=/opt/app-root/src/.cache/go-build,uid=1001 \
    CGO_ENABLED=1 GOEXPERIMENT=boringcrypto \
    go build -trimpath -ldflags="-s -w" -o bin/manager ./cmd/main.go

# Runtime stage
FROM ${BASE_IMAGE}

WORKDIR /app

# ubi9-micro doesn't include CA certificates; copy from builder for TLS (e.g. external APIs)
COPY --from=builder /build/bin/manager /app/manager

USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/app/manager"]

LABEL name="hyperfleet-operator" \
      vendor="Red Hat, Inc." \
      version="${APP_VERSION}" \
      summary="HyperFleet Operator - A Kubernetes operator that packages and delivers HyperFleet." \
      description="A Kubernetes operator that packages and delivers HyperFleet."
