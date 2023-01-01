# build image
FROM golang:alpine as build
WORKDIR /build
RUN apk add --no-cache make
COPY go.sum go.mod Makefile /build/
RUN make go-fetch
COPY . /build/
RUN make

# runtime image
FROM alpine:3.17
RUN apk add --no-cache ca-certificates
# set up nsswitch.conf for Go's "netgo" implementation
# - https://github.com/docker-library/golang/blob/1eb096131592bcbc90aa3b97471811c798a93573/1.14/alpine3.12/Dockerfile#L9
RUN if [ ! -e /etc/nsswitch.conf ];then echo 'hosts: files dns' > /etc/nsswitch.conf;fi
COPY --from=build /build/vault-unseal /usr/local/bin/vault-unseal

# runtime params
WORKDIR /
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ENV LOG_JSON=true
CMD ["vault-unseal"]
