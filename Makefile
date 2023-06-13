PWD := ${CURDIR}
PATH := $(PWD)/test/integration/hack/bin:$(PATH)
TAG?= dev
ADDITIONAL_BUILD_ARGUMENTS?=""
DOCKERFILE?="Dockerfile"

PKG		:= metacontroller
API_GROUPS := metacontroller/v1alpha1

export GO111MODULE=on
export GOTESTSUM_FORMAT=pkgname

CODE_GENERATOR_VERSION="v0.25.9"

PKGS = $(shell go list ./... | grep -v '/test/integration/\|/examples/')
COVER_PKGS = $(shell echo ${PKGS} | tr " " ",")

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: install

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: generate_crds generated_files fmt envtest ## Run tests. Add vet before envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out	

.PHONY: build
build: generated_files
	DEBUG=$(DEBUG) goreleaser build --single-target --rm-dist --snapshot --output $(PWD)/metacontroller

.PHONY: build_debug
build_debug: DEBUG=true
build_debug: build

.PHONY: unit-test
unit-test: test-setup
	@gotestsum -- -race -coverpkg="${COVER_PKGS}" -coverprofile=test/integration/hack/tmp/unit-test-coverage.out ${PKGS}

.PHONY: integration-test
integration-test: test-setup
	@cd ./test/integration; \
 	gotestsum -- -coverpkg="${COVER_PKGS}" -coverprofile=hack/tmp/integration-test-coverage.out ./... -timeout 5m -parallel 1

.PHONY: test-setup
test-setup:
	./test/integration/hack/setup.sh; \
	mkdir -p ./test/integration/hack/tmp; \

.PHONY: image
image: build
	docker build -t localhost/metacontroller:$(TAG) -f $(DOCKERFILE) .

.PHONY: image_debug
image_debug: TAG=debug
image_debug: DOCKERFILE=Dockerfile.debug
image_debug: build_debug
image_debug: image


# CRD generation
.PHONY: generate_crds
generate_crds: controller-gen
	@echo "+ Generating crds"
	@$(CONTROLLER_GEN) +crd +paths="./pkg/apis/..." +output:crd:stdout > manifests/production/metacontroller-crds-v1.yaml

# Code generators
# https://github.com/kubernetes/community/blob/master/contributors/devel/api_changes.md#generate-code

.PHONY: generated_files
generated_files: deepcopy clientset lister informer

# also builds vendored version of deepcopy-gen tool
.PHONY: deepcopy
deepcopy:
	@go install k8s.io/code-generator/cmd/deepcopy-gen@"${CODE_GENERATOR_VERSION}"
	@echo "+ Generating deepcopy funcs for $(API_GROUPS)"
	@deepcopy-gen \
		--input-dirs $(PKG)/pkg/apis/$(API_GROUPS) \
		--output-base $(PWD)/.. \
		--go-header-file ./hack/boilerplate.go.txt \
		--output-file-base zz_generated.deepcopy

# also builds vendored version of client-gen tool
.PHONY: clientset
clientset:
	@go install k8s.io/code-generator/cmd/client-gen@"${CODE_GENERATOR_VERSION}"
	@echo "+ Generating clientsets for $(API_GROUPS)"
	@client-gen \
		--fake-clientset=false \
		--go-header-file ./hack/boilerplate.go.txt \
		--input $(API_GROUPS) \
		--input-base $(PKG)/pkg/apis \
		--output-base $(PWD)/.. \
		--clientset-path $(PKG)/pkg/client/generated/clientset

# also builds vendored version of lister-gen tool
.PHONY: lister
lister:
	@go install k8s.io/code-generator/cmd/lister-gen@"${CODE_GENERATOR_VERSION}"
	@echo "+ Generating lister for $(API_GROUPS)"
	@lister-gen \
		--input-dirs $(PKG)/pkg/apis/$(API_GROUPS) \
		--go-header-file ./hack/boilerplate.go.txt \
		--output-base $(PWD)/.. \
		--output-package $(PKG)/pkg/client/generated/lister

# also builds vendored version of informer-gen tool
.PHONY: informer
informer:
	@go install k8s.io/code-generator/cmd/informer-gen@"${CODE_GENERATOR_VERSION}"
	@echo "+ Generating informer for $(API_GROUPS)"
	@informer-gen \
		--input-dirs $(PKG)/pkg/apis/$(API_GROUPS) \
		--go-header-file ./hack/boilerplate.go.txt \
		--output-base $(PWD)/.. \
		--output-package $(PKG)/pkg/client/generated/informer \
		--versioned-clientset-package $(PKG)/pkg/client/generated/clientset/internalclientset \
		--listers-package $(PKG)/pkg/client/generated/lister

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
KUSTOMIZE_VERSION ?= v5.0.0
CONTROLLER_TOOLS_VERSION ?= v0.11.3

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) --output install_kustomize.sh && bash install_kustomize.sh $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); rm install_kustomize.sh; }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
