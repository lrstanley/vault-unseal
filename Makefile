.DEFAULT_GOAL := build
BINARY=vault-unseal

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'

fetch: ## Fetches the necessary dependencies to build.
	go mod download
	go mod tidy

upgrade-deps: ## Upgrade all dependencies to the latest version.
	go get -u ./...

upgrade-deps-patch: ## Upgrade all dependencies to the latest patch release.
	go get -u=patch ./...

clean: ## Cleans up generated files/folders from the build.
	/bin/rm -rfv "dist/" "${BINARY}"

debug: clean
	go run *.go

prepare: fetch clean ## Prepare the dependencies needed for a build.

build: prepare ## Compile and generate a binary.
	CGO_ENABLED=0 go build -ldflags '-d -s -w -extldflags=-static' -tags=netgo,osusergo,static_build -installsuffix netgo -buildvcs=false -trimpath -o "${BINARY}"
