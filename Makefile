
# YEAR defines the year value used for substituting the YEAR placeholder in the boilerplate header.
YEAR ?= $(shell date +%Y)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Root directory
ROOT_DIR := $(dir $(realpath $(firstword $(MAKEFILE_LIST))))

# Tool versioning
TOOL_MOD := $(ROOT_DIR)tools/go.mod
gotool = go tool -modfile="$(TOOL_MOD)" $(1)

CONTAINER_TOOL ?= $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Container Images

# Image configuration
IMG_REGISTRY ?= quay.io/openshift-hyperfleet
IMG_NAME ?= hyperfleet-operator
APP_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")
IMG_TAG ?= $(APP_VERSION)
IMG ?= $(IMG_REGISTRY)/$(IMG_NAME):$(IMG_TAG)
GIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_DIRTY ?= $(shell [ -z "$$(git status --porcelain 2>/dev/null)" ] || echo "-modified")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# For container builds, use linux by default; override PLATFORM to build for other platforms (e.g. linux/arm64)
PLATFORM ?= linux/amd64

# Go build flags (FIPS compliant)
CGO_ENABLED ?= 1
GOFLAGS ?= -trimpath
# LDFLAGS := -s -w \
#            -X github.com/openshift-hyperfleet/hyperfleet-operator/pkg/version.Version=$(APP_VERSION) \
#            -X github.com/openshift-hyperfleet/hyperfleet-operator/pkg/version.Commit=$(GIT_SHA) \
#            -X 'github.com/openshift-hyperfleet/hyperfleet-operator/pkg/version.BuildTime=$(BUILD_DATE)'

# Dev image configuration - set QUAY_USER to push to personal registry
QUAY_USER ?=
DEV_TAG ?= dev-$(GIT_SHA)
BASE_IMAGE ?= registry.access.redhat.com/ubi9/ubi-minimal:latest

.PHONY: check-container-tool
check-container-tool:
ifndef CONTAINER_TOOL
	@echo "Error: No container tool found (docker or podman)"
	@exit 1
endif

.PHONY: image-build
image-build: check-container-tool manifests generate fmt vet ## Build container image with configurable registry/tag
	@echo "Building container image $(IMG)..."
	$(CONTAINER_TOOL) build \
		--platform $(PLATFORM) \
		--build-arg BASE_IMAGE=$(BASE_IMAGE) \
		--build-arg APP_VERSION=$(APP_VERSION) \
		-t $(IMG) .
	@echo "Image built: $(IMG)"
	@echo "$(IMG)"

.PHONY: image-push
image-push: check-container-tool ## Push container image to registry
	@echo "Pushing image $(IMG)..."
	$(CONTAINER_TOOL) push $(IMG)
	@echo "Image pushed: $(IMG)"

.PHONY: image-build-push
image-build-push: image-build image-push ## Build and push container image to registry

.PHONY: image-dev
image-build-push-dev: ## Build and push dev image to dev Quay registry (requires QUAY_USER)
ifeq ($(strip $(QUAY_USER)),)
	@echo "Error: QUAY_USER is not set"
	@echo ""
	@echo "Usage: QUAY_USER=myuser make image-dev"
	@exit 1
endif
	IMG_REGISTRY=quay.io/$(QUAY_USER) IMG_TAG=$(DEV_TAG) $(MAKE) image-build-push


##@ Development

.PHONY: manifests
manifests: ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(call gotool,controller-gen) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(call gotool,controller-gen) object:headerFile="hack/boilerplate.go.txt",year=$(YEAR) paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(call gotool,setup-envtest) use $(ENVTEST_K8S_VERSION) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# TODO(user): To use a different vendor for e2e tests, modify the setup under 'tests/e2e'.
# The default setup assumes Kind is pre-installed and builds/loads the Manager Docker image locally.
# kubectl kuberc is disabled by default for test isolation; enable with:
# - KUBECTL_KUBERC=true
# CertManager is installed by default; skip with:
# - CERT_MANAGER_INSTALL_SKIP=true
KIND_CLUSTER ?= hyperfleet-operator-test-e2e

.PHONY: setup-test-e2e
setup-test-e2e: ## Set up a Kind cluster for e2e tests if it does not exist
	@command -v $(KIND) >/dev/null 2>&1 || { \
		echo "Kind is not installed. Please install Kind manually."; \
		exit 1; \
	}
	@case "$$($(KIND) get clusters)" in \
		*"$(KIND_CLUSTER)"*) \
			echo "Kind cluster '$(KIND_CLUSTER)' already exists. Skipping creation." ;; \
		*) \
			echo "Creating Kind cluster '$(KIND_CLUSTER)'..."; \
			$(KIND) create cluster --name $(KIND_CLUSTER) ;; \
	esac
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(call gotool,setup-envtest) use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path

.PHONY: test-e2e
test-e2e: setup-test-e2e manifests generate fmt vet ## Run the e2e tests. Expected an isolated environment using Kind.
	KIND=$(KIND) KIND_CLUSTER=$(KIND_CLUSTER) go test -tags=e2e ./test/e2e/ -v -ginkgo.v
	$(MAKE) cleanup-test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the Kind cluster used for e2e tests
	@$(KIND) delete cluster --name $(KIND_CLUSTER)

.PHONY: lint
lint: ## Run golangci-lint linter
	$(call gotool,golangci-lint) run

.PHONY: lint-fix
lint-fix: ## Run golangci-lint linter and perform fixes
	$(call gotool,golangci-lint) run --fix

.PHONY: lint-config
lint-config: ## Verify golangci-lint linter configuration
	$(call gotool,golangci-lint) config verify

##@ Build

.PHONY: build
build: manifests generate fmt vet  ## Build manager binary.
	@echo "Building version: ${APP_VERSION}"
	CGO_ENABLED=$(CGO_ENABLED) GOEXPERIMENT=boringcrypto go build $(GOFLAGS) -o bin/manager ./cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

.PHONY: build-installer
build-installer: manifests generate ## Generate a consolidated YAML with CRDs and deployment.
	@mkdir -p dist
	@cd config/manager && $(call gotool,kustomize) edit set image controller=${IMG}
	@$(call gotool,kustomize) build config/default > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	@out="$$( $(call gotool,kustomize) build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | "$(KUBECTL)" apply -f -; else echo "No CRDs to install; skipping."; fi

.PHONY: uninstall
uninstall: manifests ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@out="$$( $(call gotool,kustomize) build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -; else echo "No CRDs to delete; skipping."; fi

.PHONY: deploy
deploy: manifests ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(call gotool,kustomize) edit set image controller=${IMG}
	$(call gotool,kustomize) build config/default | "$(KUBECTL)" apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(call gotool,kustomize) build config/default | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind

## Tool Versions

#ENVTEST_VERSION is the controller-runtime version to use for setup-envtest, derived from go.mod
ENVTEST_VERSION ?= $(shell v='$(call gomodver,sigs.k8s.io/controller-runtime)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_VERSION manually (controller-runtime replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v")

#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell v='$(call gomodver,k8s.io/api)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_K8S_VERSION manually (k8s.io/api replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?[0-9]+\.([0-9]+).*/1.\1/')


define gomodver
$(shell go list -m -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' $(1) 2>/dev/null)
endef
