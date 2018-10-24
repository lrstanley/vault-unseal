.DEFAULT_GOAL := build

GOPATH := $(shell go env | grep GOPATH | sed 's/GOPATH="\(.*\)"/\1/')
PATH := $(GOPATH)/bin:$(PATH)
export $(PATH)

# enable Go 1.11.x module support.
export GO111MODULE=on

BINARY=vault-unseal
VERSION=$(shell git describe --tags --abbrev=0 2>/dev/null | sed -r "s:^v::g")
COMPRESS_CONC ?= $(shell nproc)
RSRC=README_TPL.md
ROUT=README.md

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'

readme-gen: ## Generates readme from template file.
	cp -av "${RSRC}" "${ROUT}"
	sed -ri -e "s:\[\[tag\]\]:${VERSION}:g" -e "s:\[\[os\]\]:linux:g" -e "s:\[\[arch\]\]:amd64:g" "${ROUT}"

snapshot: clean fetch ## Generate a snapshot release.
	$(GOPATH)/bin/goreleaser release --snapshot --skip-validate --skip-publish

release: clean fetch ## Generate a release, but don't publish to GitHub.
	$(GOPATH)/bin/goreleaser release --skip-validate --skip-publish

publish: clean fetch ## Generate a release, and publish to GitHub.
	$(GOPATH)/bin/goreleaser release
 
fetch: ## Fetches the necessary dependencies to build.
	test -f $(GOPATH)/bin/goreleaser || go get -u -v github.com/goreleaser/goreleaser
	go mod download
	go mod tidy
	go mod vendor

clean: ## Cleans up generated files/folders from the build.
	/bin/rm -rfv "dist/" "${BINARY}"

build: fetch clean ## Compile and generate a binary.
	go build -ldflags '-d -s -w' -tags netgo -installsuffix netgo -v -x -o "${BINARY}"

debug: clean
	go run *.go
