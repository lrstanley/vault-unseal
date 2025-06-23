.DEFAULT_GOAL := build

export PROJECT := "vault-unseal"

license:
	curl -sL https://liam.sh/-/gh/g/license-header.sh | bash -s

go-fetch:
	go mod download
	go mod tidy

up:
	go get -u ./...
	go get -u -t ./...
	go mod tidy

clean:
	/bin/rm -rfv "dist/" "${PROJECT}"

go-dlv: go-prepare
	dlv debug \
		--headless --listen=:2345 \
		--api-version=2 --log \
		--allow-non-terminal-interactive \
		${PACKAGE} -- --debug

go-debug: clean
	go run *.go --debug

go-prepare: go-fetch clean
	go generate -x ./...

build: go-prepare go-fetch
	CGO_ENABLED=0 \
	go build \
		-ldflags '-d -s -w -extldflags=-static' \
		-tags=netgo,osusergo,static_build \
		-installsuffix netgo \
		-buildvcs=false \
		-trimpath \
		-o ${PROJECT}
