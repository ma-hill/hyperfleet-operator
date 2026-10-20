# Operator bundle build & `operator-sdk run bundle` — working commands

Commands used to build and test the operator bundle with `operator-sdk run
bundle` against a local kind cluster (Apple Silicon / arm64 nodes).

## Why multi-arch

Every image pushed for this flow must carry **both** `linux/amd64` and
`linux/arm64`, or one of two pulls fails:

- `operator-sdk run bundle` unpacks/validates the bundle image locally as
  **linux/amd64**, even on an arm64 Mac.
- The in-cluster unpack job pulls the bundle image with containerd, which
  enforces the node's platform (**arm64** on kind-on-Mac).

A plain `docker build` + `docker push` is always single-platform (the build
host's arch) and pushes as a platform-filtered index — the digest it prints
will fail one side or the other. Multi-platform results also cannot be loaded
into the local docker store, so multi-arch builds must `--push` directly.

## Multi-arch operator image

```sh
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --provenance=false \
  --build-arg BASE_IMAGE=registry.access.redhat.com/ubi9/ubi-minimal:latest \
  --build-arg APP_VERSION=$(git describe --tags --always --dirty) \
  --build-arg GIT_SHA=$(git rev-parse --short HEAD) \
  -t quay.io/mahill/hyperfleet-operator:demo-operator \
  --push .
```

On the HYPERFLEET-1673 branch this is wrapped as
`make image-multiarch OPERATOR_IMG=<image>`.

## Multi-arch bundle image

The dev overlay's image values come from `RELATED_IMAGE_*` env vars —
`envsubst` blanks them if they are not exported (the Makefile exports
defaults; a bare shell does not):

```sh
export RELATED_IMAGE_HYPERFLEET_OPERATOR=quay.io/mahill/hyperfleet-operator:demo-operator
export RELATED_IMAGE_HYPERFLEET_API=quay.io/redhat-services-prod/hyperfleet-tenant/hyperfleet/hyperfleet-api:latest
# branch only (Sentinel is not on main yet):
# export RELATED_IMAGE_HYPERFLEET_SENTINEL=quay.io/redhat-services-prod/hyperfleet-tenant/hyperfleet/hyperfleet-sentinel:latest

envsubst < config/manifests/dev/patch-images.yaml > config/manifests/dev/kustomization.yaml

docker buildx build -f bundle.Dockerfile \
  --platform linux/amd64,linux/arm64 \
  --provenance=false \
  --build-arg BUNDLE_VERSION=0.0.1 \
  --build-arg CHANNELS=stable \
  --build-arg DEFAULT_CHANNEL=stable \
  --build-arg KUSTOMIZE_VARIANT=config/manifests/dev \
  --build-arg VALIDATE_RELATED_IMAGES=false \
  --build-arg APP_VERSION=$(git describe --tags --always --dirty) \
  -t quay.io/mahill/hyperfleet-operator-bundle:test \
  --push .
```

On the HYPERFLEET-1673 branch this is wrapped as
`make bundle-multiarch BUNDLE_IMG=<image> RELATED_IMAGE_HYPERFLEET_OPERATOR=<image>`.

## Retag an existing multi-arch image (no rebuild)

```sh
docker buildx imagetools create -t quay.io/mahill/hyperfleet-operator-bundle:v0.0.1 \
  quay.io/mahill/hyperfleet-operator-bundle:test
```

## Verify what a tag actually holds

```sh
docker buildx imagetools inspect quay.io/mahill/hyperfleet-operator-bundle:test   # lists platforms per manifest
docker manifest inspect --verbose <image>                                          # arch of a bare (non-index) manifest
```

Both `linux/amd64` and `linux/arm64` must appear before using the image with
`run bundle`.

## Run the bundle

```sh
operator-sdk olm install                                    # once per cluster
operator-sdk run bundle quay.io/mahill/hyperfleet-operator-bundle:test -n hyperfleet
kubectl get csv,pods -n hyperfleet -w
```

Cleanup (delete CRs first, or operand owner references orphan):

```sh
kubectl delete hyperfleetconfig --all
operator-sdk cleanup hyperfleet-operator -n hyperfleet
```

## Gotchas seen in practice

- `BUNDLE_IMG` defaults to `…-bundle:v$(VERSION)` (e.g. `v0.0.1`) — running
  with the default tag is not the same tag as `:test`. Keep the tags you
  pushed multi-arch and the tag you run in sync.
- A shell-exported `OPERATOR_IMG` outranks `IMG_TAG` on the make command
  line; pass `OPERATOR_IMG=<image>` explicitly to `make image-multiarch`.
- `config/manifests/dev/kustomization.yaml` is generated and git-ignored;
  never `docker build -f bundle.Dockerfile` directly while it holds
  empty-substituted values.
- kind's default CNI does not enforce NetworkPolicy; the node has Rosetta
  binfmt registered, so bare amd64 operand images (quay `:latest`) pull and
  run under emulation, but slowly.
