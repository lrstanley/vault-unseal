.DEFAULT_GOAL := build

export PROJECT := "vault-unseal"

license:
	curl -sL https://liam.sh/-/gh/g/license-header.sh | bash -s

fetch:
	go mod download
	go mod tidy

upgrade-deps:
	go get -u ./...
	go mod tidy

upgrade-deps-patch:
	go get -u=patch ./...
	go mod tidy

clean:
	/bin/rm -rfv "dist/" "${PROJECT}"

debug: clean
	go run *.go

prepare: fetch clean
	go generate -x ./...

build: prepare fetch
	CGO_ENABLED=0 \
	go build \
		-ldflags '-d -s -w -extldflags=-static' \
		-tags=netgo,osusergo,static_build \
		-installsuffix netgo \
		-buildvcs=false \
		-trimpath \
		-o ${PROJECT}
