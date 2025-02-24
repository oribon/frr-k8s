
# Image URL to use all building/pushing image targets
IMG ?= quay.io/metallb/frr-k8s:0.1.0
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.26.1

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
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

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=daemon-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build k8s-frr binary.
	go build -o bin/frr-k8s cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish built the k8s-frr image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: test ## Build docker image with the k8s-frr.
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the k8s-frr.
	docker push ${IMG}

# PLATFORMS defines the target platforms for  the k8s-frr image be build to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - able to use docker buildx . More info: https://docs.docker.com/build/buildx/
# - have enable BuildKit, More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image for your registry (i.e. if you do not inform a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To properly provided solutions that supports more than one platform you should use this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: test ## Build and push docker image for the k8s-frr for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- docker buildx create --name project-v3-builder
	docker buildx use project-v3-builder
	- docker buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- docker buildx rm project-v3-builder
	rm Dockerfile.cross

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif


##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KIND ?= $(LOCALBIN)/kind
KUBECTL ?= $(LOCALBIN)/kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
GINKGO ?= $(LOCALBIN)/ginkgo
ENVTEST ?= $(LOCALBIN)/setup-envtest
KUBECONFIG_PATH ?= $(LOCALBIN)/kubeconfig
export KUBECONFIG=$(KUBECONFIG_PATH)

## Tool Versions
KUSTOMIZE_VERSION ?= v5.0.0
CONTROLLER_TOOLS_VERSION ?= v0.11.3
KUBECTL_VERSION ?= v1.27.0
GINKGO_VERSION ?= v2.11.0
KIND_VERSION ?= v0.19.0
KIND_CLUSTER_NAME ?= frr-k8s

.PHONY: install
install: kubectl manifests kustomize ## Install CRDs into the K8s cluster specified in $KUBECONFIG_PATH.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: kubectl manifests kustomize kind load-on-kind ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/frr-k8s && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUBECTL) -n frr-k8s-system delete ds frr-k8s-daemon || true
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -
	$(KUBECTL) -n frr-k8s-system wait --for=condition=Ready --all pods --timeout 300s

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: kubectl
kubectl: $(KUBECTL) ## Download kubectl locally if necessary. If wrong version is installed, it will be overwritten.
$(KUBECTL): $(LOCALBIN)
	test -s $(LOCALBIN)/kubectl && $(LOCALBIN)/kubectl --version | grep -q $(KUBECTL_VERSION) || \
	curl -o $(LOCALBIN)/kubectl -LO https://dl.k8s.io/release/$(KUBECTL_VERSION)/bin/$$(go env GOOS)/$$(go env GOARCH)/kubectl
	chmod +x $(LOCALBIN)/kubectl

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary. If wrong version is installed, it will be overwritten.
$(KIND): $(LOCALBIN)
	test -s $(LOCALBIN)/kind && $(LOCALBIN)/kind --version | grep -q $(KIND_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@$(KIND_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: ginkgo
ginkgo: $(GINKGO) ## Download ginkgo locally if necessary. If wrong version is installed, it will be overwritten.
$(GINKGO): $(LOCALBIN)
	test -s $(LOCALBIN)/ginkgo && $(LOCALBIN)/ginkgo version | grep -q $(GINKGO_VERSION) || \
	GOBIN=$(LOCALBIN) go install github.com/onsi/ginkgo/v2/ginkgo@$(GINKGO_VERSION)

.PHONY: e2etests
e2etests: ginkgo
	$(GINKGO) -v $(GINKGO_ARGS) ./e2etests -- $(TEST_ARGS)


.PHONY: kind-cluster
kind-cluster: kind
	KIND_BIN=$(LOCALBIN)/kind hack/kind.sh
	@echo 'kind cluster created, to use it please'
	@echo 'export KUBECONFIG=${KUBECONFIG_PATH}'

.PHONY: load-on-kind
load-on-kind: kind-cluster ## Load the docker image into the kind cluster.
	kind load docker-image ${IMG} -n ${KIND_CLUSTER_NAME}

.PHONY: lint
lint:
	hack/lint.sh

.PHONY: bumplicense
bumplicense:
	hack/bumplicense.sh

.PHONY: checkuncommitted
checkuncommitted:
	git diff --exit-code

.PHONY: bumpall
bumpall: bumplicense manifests
	go mod tidy

KIND_EXPORT_LOGS ?=/tmp/kind_logs

.PHONY: kind-export-logs
kind-export-logs:
	$(LOCALBIN)/kind export logs --name ${KIND_CLUSTER_NAME} ${KIND_EXPORT_LOGS}
