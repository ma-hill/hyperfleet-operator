# Building stage
FROM registry.access.redhat.com/openshift4/ose-operator-framework-tools-rhel9@sha256:785270e976b3b080acaf0d8a22c268e41392b3690ce6a391823d640915f17b39 AS builder

WORKDIR /workspace

# COPY template file set as a build-arg
# Supports konflux + dev builds
ARG TEMPLATEFILE
COPY catalog/base-template.yaml ./
COPY catalog/${TEMPLATEFILE} ./

RUN cat base-template.yaml ${TEMPLATEFILE} > ./template.yaml

RUN /bin/opm alpha render-template basic \
        --migrate-level=bundle-object-to-csv-metadata \
        -o yaml ./template.yaml > catalog.yaml

# Final serving stage
FROM registry.access.redhat.com/openshift4/ose-operator-framework-tools-rhel9@sha256:785270e976b3b080acaf0d8a22c268e41392b3690ce6a391823d640915f17b39 AS serve

COPY --from=builder /workspace/catalog.yaml /configs/hyperfleet-operator/catalog.yaml

RUN ["/bin/opm", "serve", "/configs/hyperfleet-operator", "--cache-dir=/tmp/cache", "--cache-only"]

ENTRYPOINT ["/bin/opm"]
CMD ["serve", "/configs/hyperfleet-operator", "--cache-dir=/tmp/cache"]

ARG APP_VERSION="0.0.0-dev"
LABEL version="${APP_VERSION}"
LABEL operators.operatorframework.io.index.configs.v1=/configs
