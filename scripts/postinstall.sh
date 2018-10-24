#!/bin/bash

# 1 == install
# 2 == upgrade

chmod -v 600 /etc/vault-unseal.yaml

systemctl daemon-reload
systemctl enable vault-unseal.service
systemctl restart vault-unseal.service
#systemctl try-restart vault-unseal.service
#systemctl start vault-unseal.service
