ARG BASE_IMAGE=registry.access.redhat.com/ubi9-micro:latest

FROM registry.access.redhat.com/ubi9/go-toolset:9.8-1786351949 AS builder

ARG APP_VERSION="0.0.0-dev"

USER root
WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/


RUN CGO_ENABLED=1 GOEXPERIMENT=boringcrypto \
    go build -trimpath -ldflags="-s -w" -o manager ./cmd/main.go

# Runtime stage
FROM ${BASE_IMAGE} as final

WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]

LABEL name="hyperfleet-operator" \
      vendor="Red Hat, Inc." \
      version="${APP_VERSION}" \
      summary="HyperFleet Operator - A Kubernetes operator that packages and delivers HyperFleet." \
      description="A Kubernetes operator that packages and delivers HyperFleet."