# syntax = docker/dockerfile:1.4

# build image
FROM golang:alpine as build
WORKDIR /build

RUN apk add --no-cache make
COPY go.sum go.mod Makefile /build/
RUN \
	--mount=type=cache,target=/root/.cache \
	--mount=type=cache,target=/go \
	make go-fetch

COPY . /build/
RUN \
	--mount=type=cache,target=/root/.cache \
	--mount=type=cache,target=/go \
	make

# runtime image
FROM alpine:3.23
RUN apk add --no-cache ca-certificates
COPY --from=build /build/vault-unseal /usr/local/bin/vault-unseal

# runtime params
WORKDIR /
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ENV LOG_JSON=true
CMD ["/usr/local/bin/vault-unseal"]
