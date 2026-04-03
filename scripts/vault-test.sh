#!/bin/bash

docker run \
    -it \
    --rm \
    --cap-add=IPC_LOCK \
    -p 8200:8200 \
    -e 'VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200' \
    hashicorp/vault:latest
